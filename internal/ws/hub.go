package ws

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/command"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"google.golang.org/protobuf/proto"
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

// Hub manages WebSocket connections and log subscriptions
type Hub struct {
	logStreamer *logger.LogStreamer
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	store       *storage.Store
	docker      *docker.Client
	log         *logger.Logger
	sender      *command.Sender

	upgrader websocket.Upgrader

	// Active clients
	clients   map[*Client]bool
	clientsMu sync.RWMutex

	// Register/unregister channels
	register   chan *Client
	unregister chan *Client
}

// Client represents a single WebSocket connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

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
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins (CORS handled elsewhere)
			},
		},
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			h.log.Debug("WebSocket client connected")

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
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

// ServeHTTP handles WebSocket upgrade requests
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*subscription),
	}

	h.register <- client

	// Start read/write pumps
	go client.writePump()
	go client.readPump()
}

// readPump reads messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.cleanup()
		c.hub.unregister <- c
		c.conn.Close()
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
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

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

// handleAuth authenticates the client
func (c *Client) handleAuth(msg *v1.AuthMessage) {
	if msg == nil {
		c.sendAuthFail("missing auth message")
		return
	}

	// If no auth providers are enabled, bypass auth entirely - grant full admin access
	if !c.hub.authManager.IsAnyAuthEnabled() {
		c.user = &auth.AuthenticatedUser{
			ID:       "admin",
			Username: "admin",
			Roles:    []string{"admin"},
			Provider: "none",
		}
		c.authenticated = true
		c.sendAuthOk()
		return
	}

	ctx := context.Background()

	if msg.Token != "" {
		var user *auth.AuthenticatedUser
		var err error
		if strings.HasPrefix(msg.Token, "dp_") {
			user, err = c.hub.authManager.ValidateAPIToken(ctx, msg.Token)
		} else {
			user, err = c.hub.authManager.ValidateSession(ctx, msg.Token)
		}
		if err != nil {
			// Try anonymous access
			if c.hub.authManager.IsAnonymousAccessEnabled() {
				c.user = c.hub.authManager.AnonymousUser()
				c.authenticated = true
				c.sendAuthOk()
				return
			}
			c.sendAuthFail("invalid token")
			return
		}
		c.user = user
		c.authenticated = true
		c.sendAuthOk()
	} else if c.hub.authManager.IsAnonymousAccessEnabled() {
		c.user = c.hub.authManager.AnonymousUser()
		c.authenticated = true
		c.sendAuthOk()
	} else {
		c.sendAuthFail("authentication required")
	}
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

	// Check permission
	if c.hub.enforcer != nil && c.user != nil {
		allowed, err := c.hub.enforcer.Enforce(c.user.Roles, rbac.ResourceServers, rbac.ActionRead, msg.ServerId)
		if err != nil || !allowed {
			c.sendError("permission denied")
			return
		}
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
	if _, exists := c.subscriptions[msg.ServerId]; !exists {
		// Subscribe to log streamer
		ch := c.hub.logStreamer.Subscribe(server.ContainerID)
		c.subscriptions[msg.ServerId] = &subscription{ch: ch, containerID: server.ContainerID}
		go c.forwardLogs(msg.ServerId, ch)
	}
	c.subscriptionsMu.Unlock()

	// Send initial logs
	logs := c.hub.logStreamer.GetLogs(server.ContainerID, tail)
	c.sendLogs(msg.ServerId, logs)

	// Confirm subscription
	c.sendSubscribed(msg.ServerId)
}

// forwardLogs forwards log entries from the log streamer to the client
func (c *Client) forwardLogs(serverId string, ch chan *v1.LogEntry) {
	for entry := range ch {
		c.sendLog(serverId, entry)
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

	// Check command permission
	if c.hub.enforcer != nil && c.user != nil {
		allowed, err := c.hub.enforcer.Enforce(c.user.Roles, rbac.ResourceServers, rbac.ActionCommand, msg.ServerId)
		if err != nil || !allowed {
			c.sendCommandResult(msg.ServerId, false, "", "permission denied")
			return
		}
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
	status, err := c.hub.docker.GetContainerStatus(ctx, server.ContainerID)
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

// sendMessage marshals and sends a server message
func (c *Client) sendMessage(msg *v1.WebSocketServerMessage) {
	data, err := proto.Marshal(msg)
	if err != nil {
		c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		// Channel full, skip
	}
}

func (c *Client) sendAuthOk() {
	userId := ""
	username := ""
	if c.user != nil {
		userId = c.user.ID
		username = c.user.Username
	}
	c.sendMessage(&v1.WebSocketServerMessage{
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
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_AUTH_FAIL,
		Payload: &v1.WebSocketServerMessage_AuthFail{
			AuthFail: &v1.AuthFailMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendSubscribed(serverId string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Subscribed{
			Subscribed: &v1.SubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendUnsubscribed(serverId string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Unsubscribed{
			Unsubscribed: &v1.UnsubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendLogs(serverId string, logs []*v1.LogEntry) {
	c.sendMessage(&v1.WebSocketServerMessage{
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
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOG,
		Payload: &v1.WebSocketServerMessage_Log{
			Log: &v1.LogMessage{
				ServerId: serverId,
				Log:      log,
			},
		},
	})
}

func (c *Client) sendCommandResult(serverId string, success bool, output, errMsg string) {
	c.sendMessage(&v1.WebSocketServerMessage{
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
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_ERROR,
		Payload: &v1.WebSocketServerMessage_Error{
			Error: &v1.ErrorMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendPong() {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_PONG,
	})
}
