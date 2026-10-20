// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package ws

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/command"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024 // 512KB
)

// controlSendTimeout bounds how long a control message waits for room in a
// congested client channel. Log lines are droppable (see sendLog); control
// messages (auth results, acks, errors) block up to this long instead. It is
// a var so tests can shrink it.
var controlSendTimeout = 5 * time.Second

// Hub manages WebSocket connections and log subscriptions
type Hub struct {
	logStreamer *logger.LogStreamer
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	store       *storage.Store
	docker      *docker.Client
	pool        *docker.ClientPool
	poolMu      sync.RWMutex
	log         *logger.Logger
	sender      *command.Sender

	upgrader websocket.Upgrader

	// Active clients
	clients   map[*Client]bool
	clientsMu sync.RWMutex

	// Register/unregister channels
	register   chan *Client
	unregister chan *Client

	// quit terminates the Run loop and releases every producer that would
	// otherwise block forever on the unbuffered register/unregister channels.
	//
	// ServeHTTP sent on h.register unconditionally. With no Run loop running -
	// or after it exited - that send never completed, hanging the HTTP handler
	// goroutine indefinitely. readPump's unregister send had the same problem.
	quit     chan struct{}
	quitOnce sync.Once

	// registerTimeout bounds how long a handshake waits to be accepted by the
	// Run loop. A request goroutine must never block indefinitely on internal
	// coordination: if the loop is wedged or was never started, the connection
	// is rejected instead of leaking the handler.
	registerTimeout time.Duration
}

// defaultRegisterTimeout bounds handshake acceptance by the hub loop.
const defaultRegisterTimeout = 5 * time.Second

// Client represents a single WebSocket connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// done is closed exactly once when the client is unregistered. It
	// signals writePump to exit. send is deliberately NEVER closed: a
	// forwardLogs goroutine may still be selecting on it while unregister
	// runs, and a send on a closed channel panics even inside a select.
	// The send channel is simply garbage-collected once every sender has
	// stopped observing done.
	done      chan struct{}
	closeOnce sync.Once

	// droppedLogs counts log lines discarded because send was full. The
	// count is injected as a "… N lines skipped …" sentinel before the next
	// delivered line so bursts are visible instead of silent. Touched from
	// forwardLogs goroutines and the read pump; hence atomic.
	droppedLogs atomic.Uint64

	// Authentication
	user          *auth.AuthenticatedUser
	authenticated bool

	// Subscriptions: serverId -> subscription (log channel + the containerID
	// it was registered under in the log streamer)
	subscriptions   map[string]*subscription
	subscriptionsMu sync.RWMutex
}

// subscription tracks a client's log subscription for a server. The
// containerID is captured at subscribe time so unsubscribe/cleanup can
// always route through the LogStreamer with the exact key the channel was
// registered under — even if the server row has since been deleted or the
// container has changed. This avoids re-deriving the containerID from a
// fresh DB lookup, which can fail (server deleted) and previously led to
// closing an already-closed channel.
type subscription struct {
	ch          chan *v1.LogEntry
	containerID string
}

// NewHub creates a new WebSocket hub
func NewHub(logStreamer *logger.LogStreamer, authManager *auth.Manager, enforcer *rbac.Enforcer, store *storage.Store, docker *docker.Client, sender *command.Sender, log *logger.Logger) *Hub {
	return &Hub{
		logStreamer: logStreamer,
		authManager: authManager,
		enforcer:    enforcer,
		store:       store,
		docker:      docker,
		log:         log,
		sender:      sender,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return checkOrigin(r, originPolicy{}) },
		},
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),

		registerTimeout: defaultRegisterTimeout,
	}
}

// SetPool sets the Docker client pool for multi-node support.
func (h *Hub) SetPool(pool *docker.ClientPool) {
	h.poolMu.Lock()
	defer h.poolMu.Unlock()
	h.pool = pool
}

// dockerFor returns the Docker client responsible for the given nodeID.
// If the pool is configured, GetClientStrict is attempted; if nodeID is empty or
// resolution fails, it falls back to the default Docker client.
func (h *Hub) dockerFor(nodeID string) *docker.Client {
	h.poolMu.RLock()
	pool := h.pool
	h.poolMu.RUnlock()

	if pool != nil && nodeID != "" {
		if cli, err := pool.GetClientStrict(nodeID); err == nil && cli != nil {
			return cli
		}
	}
	return h.docker
}

// SetCheckOrigin overrides the WebSocket upgrader origin validation function.
func (h *Hub) SetCheckOrigin(check func(r *http.Request) bool) {
	h.upgrader.CheckOrigin = check
}

// SetOriginPolicy sets the browser origins that may open a console socket.
// An empty allowlist keeps same-host checks. X-Forwarded-Host is ignored
// unless trustProxy is set. A missing Origin header stays allowed for CLI clients.
func (h *Hub) SetOriginPolicy(allow []string, trustProxy bool) {
	policy := originPolicy{allow: append([]string(nil), allow...), trustProxy: trustProxy}
	h.upgrader.CheckOrigin = func(r *http.Request) bool {
		return checkOrigin(r, policy)
	}
}

// sendRegister hands a client to the Run loop, reporting false rather than
// blocking indefinitely when the loop is not running, has stopped, or the
// request context is already cancelled.
func (h *Hub) sendRegister(ctx context.Context, client *Client) bool {
	timeout := h.registerTimeout
	if timeout <= 0 {
		timeout = defaultRegisterTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case h.register <- client:
		return true
	case <-h.quit:
		h.log.Debug("WebSocket register rejected: hub stopped")
		return false
	case <-ctx.Done():
		return false
	case <-timer.C:
		h.log.Error("WebSocket register rejected: hub did not accept the client within %v", timeout)
		return false
	}
}

// sendUnregister releases a client from the Run loop without ever blocking the
// caller: a pump tearing down after the hub stopped must not hang.
func (h *Hub) sendUnregister(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.quit:
		h.log.Debug("WebSocket unregister skipped: hub stopped")
	}
}

// Stop terminates the Run loop, closes every connected client's done channel so
// their pumps exit, and releases anything blocked on register/unregister.
// Safe to call more than once and safe to call without Run having started.
func (h *Hub) Stop() {
	h.quitOnce.Do(func() {
		close(h.quit)

		h.clientsMu.RLock()
		clients := make([]*Client, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		h.clientsMu.RUnlock()

		for _, c := range clients {
			if c == nil {
				continue
			}
			c.closeOnce.Do(func() { close(c.done) })
			// A client can be tracked before its pump has a live connection
			// (and tests construct clients without one), so nil is expected.
			if c.conn != nil {
				_ = c.conn.Close()
			}
		}
	})
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case <-h.quit:
			h.log.Debug("WebSocket hub stopped")
			return

		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			h.log.Debug("WebSocket client connected")

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				// Never close(client.send): forwardLogs may still send on
				// it. Signal done instead; writePump exits and the channel
				// is garbage-collected once all senders stop.
				client.closeOnce.Do(func() { close(client.done) })
			}
			h.clientsMu.Unlock()
			h.log.Debug("WebSocket client disconnected")
		}
	}
}

// MigrateServerContainer re-points every live subscription for serverID at the
// container's new ID (MINE-108). Without this, a client that subscribed before
// a container recreation keeps the old containerID, so a later unsubscribe or
// disconnect cleanup asks the LogStreamer to close a channel under a key it no
// longer knows about — leaking the channel and its forwarder goroutine.
//
// It is called by the reconciler once it has observed (or performed) a
// recreation, and is a no-op for clients with no subscription for serverID.
func (h *Hub) MigrateServerContainer(serverID, newContainerID string) {
	if serverID == "" || newContainerID == "" {
		return
	}

	h.clientsMu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clientsMu.RUnlock()

	for _, c := range clients {
		c.migrateSubscription(serverID, newContainerID)
	}
}

// migrateSubscription updates the container key recorded for serverID's
// subscription. The LogStreamer has already moved the channel itself (or will
// via its own alias), so only the bookkeeping used on unsubscribe/cleanup needs
// to change.
func (c *Client) migrateSubscription(serverID, newContainerID string) {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	if sub, ok := c.subscriptions[serverID]; ok {
		sub.containerID = newContainerID
	}
}

// Pre-upgrade credential failures (MINE-164). The handshake is rejected
// with 401 before a socket exists; the AUTH message path reports the same
// failures over the socket it already holds.
var (
	errWSNoAuthProvider = errors.New("no authentication provider is enabled")
	errWSInvalidToken   = errors.New("invalid token")
	errWSAuthRequired   = errors.New("authentication required")
)

// authenticateToken applies the WS credential policy shared by the
// pre-upgrade handshake check and the AUTH message path: explicit no-auth
// mode, session/API tokens, then the anonymous fallback.
func (h *Hub) authenticateToken(ctx context.Context, token string) (*auth.AuthenticatedUser, error) {
	// If no auth providers are enabled the panel is running without
	// authentication. That is only permitted when the operator explicitly
	// opted in (auth.allow_no_auth); otherwise fail closed rather than grant
	// every client admin.
	if !h.authManager.IsAnyAuthEnabled() {
		if !h.authManager.NoAuthAllowed() {
			return nil, errWSNoAuthProvider
		}
		return &auth.AuthenticatedUser{
			ID:       "admin",
			Username: "admin",
			Roles:    []string{"admin"},
			Provider: "none",
		}, nil
	}

	if token != "" {
		var user *auth.AuthenticatedUser
		var err error
		if strings.HasPrefix(token, "dp_") {
			user, err = h.authManager.ValidateAPIToken(ctx, token)
		} else {
			user, err = h.authManager.ValidateSession(ctx, token)
		}
		if err == nil {
			return user, nil
		}
		// Try anonymous access
		if h.authManager.IsAnonymousAccessEnabled() {
			return h.authManager.AnonymousUser(), nil
		}
		return nil, errWSInvalidToken
	}
	if h.authManager.IsAnonymousAccessEnabled() {
		return h.authManager.AnonymousUser(), nil
	}
	return nil, errWSAuthRequired
}

// handshakeToken extracts the pre-upgrade credential from the Authorization
// header or the cp_session cookie. Session JWTs in the query string are
// ignored (MINE-185). The OIDC browser redirect /login?token= is the one
// remaining place a session token appears in a URL; it is not accepted here.
func handshakeToken(r *http.Request) string {
	return auth.SessionTokenFromRequest(r)
}

// ServeHTTP handles WebSocket upgrade requests
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate before upgrading (MINE-164): unauthenticated callers
	// get 401 and never hold a socket. The credential policy matches the
	// AUTH message path exactly (see authenticateToken).
	user, err := h.authenticateToken(r.Context(), handshakeToken(r))
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*subscription),
		// Pre-authenticated from the handshake; the client may still send
		// AUTH to refresh its identity.
		user:          user,
		authenticated: true,
	}

	if !h.sendRegister(r.Context(), client) {
		_ = conn.Close()
		return
	}

	// Start read/write pumps
	go client.writePump()
	go client.readPump()
}

// readPump reads messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.cleanup()
		c.hub.sendUnregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.log.Error("WebSocket read error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump writes messages to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *Client) handleMessage(data []byte) {
	msg := &v1.WebSocketClientMessage{}
	if err := proto.Unmarshal(data, msg); err != nil {
		c.hub.log.Error("Failed to unmarshal WebSocket message: %v", err)
		c.sendError("invalid message format")
		return
	}

	switch msg.Type {
	case v1.WSMessageType_WS_MESSAGE_TYPE_AUTH:
		c.handleAuth(msg.GetAuth())
	case v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBE:
		c.handleSubscribe(msg.GetSubscribe())
	case v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBE:
		c.handleUnsubscribe(msg.GetUnsubscribe())
	case v1.WSMessageType_WS_MESSAGE_TYPE_COMMAND:
		c.handleCommand(msg.GetCommand())
	case v1.WSMessageType_WS_MESSAGE_TYPE_PING:
		c.sendPong()
	default:
		c.sendError("unknown message type")
	}
}

// handleAuth (re-)authenticates the client over an existing socket using
// the same policy as the pre-upgrade handshake check.
func (c *Client) handleAuth(msg *v1.AuthMessage) {
	if msg == nil {
		c.sendAuthFail("missing auth message")
		return
	}

	user, err := c.hub.authenticateToken(context.Background(), msg.Token)
	if err != nil {
		c.sendAuthFail(err.Error())
		return
	}
	c.user = user
	c.authenticated = true
	c.sendAuthOk()
}

// handleSubscribe subscribes to server logs
func (c *Client) handleSubscribe(msg *v1.SubscribeMessage) {
	if !c.authenticated {
		c.sendError("not authenticated")
		return
	}

	if msg == nil || msg.ServerId == "" {
		c.sendError("missing server_id")
		return
	}

	// Check permission. Fail closed when the enforcer is unavailable rather
	// than skipping the check (the RPC interceptor denies in the same case).
	if c.hub.enforcer == nil || c.user == nil {
		c.sendError("permission denied")
		return
	}
	if allowed, err := c.hub.enforcer.Enforce(c.user.Roles, rbac.ResourceServers, rbac.ActionRead, msg.ServerId); err != nil || !allowed {
		c.sendError("permission denied")
		return
	}

	// Get server to find container ID
	ctx := context.Background()
	server, err := c.hub.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		c.sendError("server not found")
		return
	}

	tail := int(msg.Tail)
	if tail <= 0 {
		tail = 500
	}

	// If server has no container yet (just created, never started),
	// send empty logs and confirm subscription without starting streaming.
	// The client will re-subscribe when the server status changes.
	if server.ContainerID == "" {
		c.sendLogs(msg.ServerId, nil)
		c.sendSubscribed(msg.ServerId)
		return
	}

	// Ensure log streaming is active for this container
	if err := c.hub.logStreamer.StartStreaming(server.ContainerID); err != nil {
		c.hub.log.Warn("Failed to start log streaming for container %s: %v", server.ContainerID, err)
	}

	// Check if already subscribed
	c.subscriptionsMu.Lock()
	if sub, exists := c.subscriptions[msg.ServerId]; !exists {
		// Subscribe to log streamer
		ch := c.hub.logStreamer.Subscribe(server.ContainerID)
		c.subscriptions[msg.ServerId] = &subscription{ch: ch, containerID: server.ContainerID}
		go c.forwardLogs(msg.ServerId, ch)
	} else if sub.containerID != server.ContainerID {
		// Container recreated or adopted: rebind subscription to current container
		c.hub.logStreamer.Unsubscribe(sub.containerID, sub.ch)
		ch := c.hub.logStreamer.Subscribe(server.ContainerID)
		sub.containerID = server.ContainerID
		sub.ch = ch
		go c.forwardLogs(msg.ServerId, ch)
	}
	c.subscriptionsMu.Unlock()

	// Send initial logs
	logs := c.hub.logStreamer.GetLogs(server.ContainerID, tail)
	c.sendLogs(msg.ServerId, logs)

	// Confirm subscription
	c.sendSubscribed(msg.ServerId)
}

// forwardLogs forwards log entries from the log streamer to the client.
// It also exits when the client is unregistered so the goroutine cannot
// outlive the client when the streamer channel stays open.
func (c *Client) forwardLogs(serverId string, ch chan *v1.LogEntry) {
	for {
		select {
		case <-c.done:
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			c.sendLog(serverId, entry)
		}
	}
}

// handleUnsubscribe unsubscribes from server logs
func (c *Client) handleUnsubscribe(msg *v1.UnsubscribeMessage) {
	if msg == nil || msg.ServerId == "" {
		c.sendError("missing server_id")
		return
	}

	// Always clean up the subscription. Route through the LogStreamer using
	// the containerID captured at subscribe time rather than re-resolving it
	// via the DB: if the server has since been deleted, the LogStreamer has
	// already closed this channel (via RemoveContainer), so closing it again
	// here directly would panic. LogStreamer.Unsubscribe is a safe no-op in
	// that case.
	c.subscriptionsMu.Lock()
	if sub, exists := c.subscriptions[msg.ServerId]; exists {
		delete(c.subscriptions, msg.ServerId)
		c.hub.logStreamer.Unsubscribe(sub.containerID, sub.ch)
	}
	c.subscriptionsMu.Unlock()

	c.sendUnsubscribed(msg.ServerId)
}

// handleCommand executes a command on the server
func (c *Client) handleCommand(msg *v1.CommandMessage) {
	if !c.authenticated {
		c.sendError("not authenticated")
		return
	}

	if msg == nil || msg.ServerId == "" || msg.Command == "" {
		c.sendError("missing server_id or command")
		return
	}

	silent := false
	if msg.Silent != nil {
		silent = *msg.Silent
	}

	// Check command permission. Fail closed when the enforcer is unavailable.
	if c.hub.enforcer == nil || c.user == nil {
		c.sendCommandResult(msg.ServerId, false, "", "permission denied")
		return
	}
	if allowed, err := c.hub.enforcer.Enforce(c.user.Roles, rbac.ResourceServers, rbac.ActionCommand, msg.ServerId); err != nil || !allowed {
		c.sendCommandResult(msg.ServerId, false, "", "permission denied")
		return
	}

	ctx := context.Background()
	server, err := c.hub.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		c.sendCommandResult(msg.ServerId, false, "", "server not found")
		return
	}

	if server.ContainerID == "" {
		c.sendCommandResult(msg.ServerId, false, "", "server has no container")
		return
	}

	// Check server status
	dockerCli := c.hub.dockerFor(server.NodeID)
	if dockerCli == nil {
		c.sendCommandResult(msg.ServerId, false, "", "node unavailable")
		return
	}
	status, err := dockerCli.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || status != storage.StatusRunning {
		c.sendCommandResult(msg.ServerId, false, "", "server is not running")
		return
	}

	// Add command to log stream if not silent
	commandTime := time.Now()
	if !silent {
		c.hub.logStreamer.AddCommandEntry(server.ContainerID, msg.Command, commandTime)
	}

	output, err := c.hub.sender.SendCommand(ctx, server.ID, msg.Command)
	success := err == nil

	// Add output to log stream if not silent
	if !silent && (output != "" || !success) {
		c.hub.logStreamer.AddCommandOutput(server.ContainerID, output, success, commandTime)
	}

	if err != nil {
		c.sendCommandResult(msg.ServerId, false, "", err.Error())
		return
	}

	c.sendCommandResult(msg.ServerId, true, output, "")
}

// cleanup removes all subscriptions when client disconnects
func (c *Client) cleanup() {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	for _, sub := range c.subscriptions {
		c.hub.logStreamer.Unsubscribe(sub.containerID, sub.ch)
	}
	c.subscriptions = make(map[string]*subscription)
}

// sendMessage marshals and best-effort enqueues a server message,
// dropping it if the client's send buffer is full. Log lines use this
// (see sendLog); control messages must use sendControl instead.
func (c *Client) sendMessage(msg *v1.WebSocketServerMessage) {
	data, err := proto.Marshal(msg)
	if err != nil {
		c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		return
	}
	c.trySend(data)
}

// trySend enqueues one marshalled message, reporting whether buffer space
// was available. Single non-blocking select shared by the log path so drop
// accounting stays exact under concurrent forwardLogs goroutines.
func (c *Client) trySend(data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// sendControl marshals and delivers a control message (auth, acks, errors,
// command results, history batches), waiting up to controlSendTimeout for
// buffer space instead of dropping it like log lines. A stuck socket can
// delay the caller briefly but control traffic is tiny and rare, while a
// dropped AUTH_OK or error would leave the console in a wrong state.
func (c *Client) sendControl(msg *v1.WebSocketServerMessage) {
	data, err := proto.Marshal(msg)
	if err != nil {
		c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		return
	}

	select {
	case c.send <- data:
	case <-time.After(controlSendTimeout):
		c.hub.log.Warn("Dropped control message to slow WebSocket client (buffer full for %v)", controlSendTimeout)
	}
}

func (c *Client) sendAuthOk() {
	userId := ""
	username := ""
	if c.user != nil {
		userId = c.user.ID
		username = c.user.Username
	}
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_AUTH_OK,
		Payload: &v1.WebSocketServerMessage_AuthOk{
			AuthOk: &v1.AuthOkMessage{
				UserId:   userId,
				Username: username,
			},
		},
	})
}

func (c *Client) sendAuthFail(errMsg string) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_AUTH_FAIL,
		Payload: &v1.WebSocketServerMessage_AuthFail{
			AuthFail: &v1.AuthFailMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendSubscribed(serverId string) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Subscribed{
			Subscribed: &v1.SubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendUnsubscribed(serverId string) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Unsubscribed{
			Unsubscribed: &v1.UnsubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendLogs(serverId string, logs []*v1.LogEntry) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOGS,
		Payload: &v1.WebSocketServerMessage_Logs{
			Logs: &v1.LogsMessage{
				ServerId: serverId,
				Logs:     logs,
			},
		},
	})
}

func (c *Client) sendLog(serverId string, log *v1.LogEntry) {
	// Flush a pending skip notice first so the gap is visible in the
	// console instead of vanishing silently (MINE-165). If the notice
	// itself drops, the count is re-armed and reported with the next
	// delivered line.
	if skipped := c.droppedLogs.Swap(0); skipped > 0 {
		if notice, err := proto.Marshal(&v1.WebSocketServerMessage{
			Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOG,
			Payload: &v1.WebSocketServerMessage_Log{
				Log: &v1.LogMessage{
					ServerId: serverId,
					Log: &v1.LogEntry{
						Timestamp: timestamppb.New(time.Now()),
						Message:   fmt.Sprintf("… %d log lines skipped (client slow, buffer full) …", skipped),
						Level:     "warn",
						Source:    "panel",
					},
				},
			},
		}); err != nil {
			c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		} else if !c.trySend(notice) {
			c.droppedLogs.Add(skipped)
		}
	}

	data, err := proto.Marshal(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOG,
		Payload: &v1.WebSocketServerMessage_Log{
			Log: &v1.LogMessage{
				ServerId: serverId,
				Log:      log,
			},
		},
	})
	if err != nil {
		c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		return
	}

	if !c.trySend(data) {
		c.droppedLogs.Add(1)
	}
}

func (c *Client) sendCommandResult(serverId string, success bool, output, errMsg string) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_COMMAND_RESULT,
		Payload: &v1.WebSocketServerMessage_CommandResult{
			CommandResult: &v1.CommandResultMessage{
				ServerId: serverId,
				Success:  success,
				Output:   output,
				Error:    errMsg,
			},
		},
	})
}

func (c *Client) sendError(errMsg string) {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_ERROR,
		Payload: &v1.WebSocketServerMessage_Error{
			Error: &v1.ErrorMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendPong() {
	c.sendControl(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_PONG,
	})
}

// originPolicy is the WebSocket browser-origin rule (MINE-208).
type originPolicy struct {
	allow      []string
	trustProxy bool
}

// checkSameOrigin is the default policy: same host, no forwarded-host trust.
func checkSameOrigin(r *http.Request) bool {
	return checkOrigin(r, originPolicy{})
}

// checkOrigin verifies that the request Origin matches the request Host,
// a trusted forwarded host, or an explicit allowlist entry.
// Non-browser clients omitting the Origin header are permitted.
func checkOrigin(r *http.Request, policy originPolicy) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Non-browser clients (CLI, curl, native tests) don't send Origin
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// Must be an http or https scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	if originListed(origin, u, policy.allow) {
		return true
	}

	// Exact host:port match with request Host
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}

	// X-Forwarded-Host is attacker-controlled unless the operator trusts the proxy.
	if policy.trustProxy {
		if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
			hosts := strings.Split(xfh, ",")
			firstHost := strings.TrimSpace(hosts[0])
			if strings.EqualFold(u.Host, firstHost) {
				return true
			}
			originHost := u.Hostname()
			if fHost, _, err := net.SplitHostPort(firstHost); err == nil {
				if strings.EqualFold(originHost, fHost) {
					return true
				}
			} else if strings.EqualFold(originHost, firstHost) {
				return true
			}
		}
	}

	// Normalize host and port for default HTTP(S) ports
	originHost := u.Hostname()
	originPort := u.Port()
	if originPort == "" {
		if u.Scheme == "https" {
			originPort = "443"
		} else {
			originPort = "80"
		}
	}

	reqHost, reqPort, err := net.SplitHostPort(r.Host)
	if err != nil {
		reqHost = r.Host
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			reqPort = "443"
		} else {
			reqPort = "80"
		}
	}

	if strings.EqualFold(originHost, reqHost) && originPort == reqPort {
		return true
	}

	// Allow loopback-to-loopback connections for local development
	if isLoopbackHost(originHost) && isLoopbackHost(reqHost) {
		return true
	}

	return false
}

func originListed(origin string, u *url.URL, allow []string) bool {
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, origin) || strings.EqualFold(entry, u.Host) || strings.EqualFold(entry, u.Hostname()) {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
