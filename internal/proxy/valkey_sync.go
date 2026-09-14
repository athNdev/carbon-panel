package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

const (
	ValkeyChannelRoutingEvents = "CARBONPANEL:routing:events"
	ValkeyRouteKeyPrefix       = "CARBONPANEL:routes:"
)

// RouteInfo holds stored route details in Valkey / Dragonfly KV
type RouteInfo struct {
	ServerID    string `json:"server_id"`
	Hostname    string `json:"hostname"`
	BackendHost string `json:"backend_host"`
	BackendPort int    `json:"backend_port"`
	ProxyPort   int    `json:"proxy_port"`
	UpdatedAt   int64  `json:"updated_at"`
}

// RoutingEvent represents a pub/sub broadcast event for route synchronization
type RoutingEvent struct {
	Action      string `json:"action"` // "add", "update", "remove"
	ServerID    string `json:"server_id"`
	Hostname    string `json:"hostname"`
	BackendHost string `json:"backend_host,omitempty"`
	BackendPort int    `json:"backend_port,omitempty"`
	ProxyPort   int    `json:"proxy_port"`
	OriginNode  string `json:"origin_node"`
	Timestamp   int64  `json:"timestamp"`
}

// ValkeyClient is an interface for Valkey/Dragonfly KV and pub/sub operations
type ValkeyClient interface {
	Ping(ctx context.Context) error
	SetRoute(ctx context.Context, hostname string, route *RouteInfo) error
	GetRoute(ctx context.Context, hostname string) (*RouteInfo, error)
	DeleteRoute(ctx context.Context, hostname string) error
	Publish(ctx context.Context, channel string, message string) error
	Subscribe(ctx context.Context, channel string, onMessage func(channel, message string)) error
	Close() error
}

// RespClient implements a lightweight Go-native Redis RESP protocol client
type RespClient struct {
	addr     string
	password string
	mu       sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
	subConn  net.Conn
	subChan  chan struct{}
}

// NewRespClient creates a new RESP client for Valkey or Dragonfly
func NewRespClient(addr string, password ...string) *RespClient {
	cleanAddr := addr
	if strings.HasPrefix(cleanAddr, "redis://") {
		cleanAddr = strings.TrimPrefix(cleanAddr, "redis://")
	} else if strings.HasPrefix(cleanAddr, "valkey://") {
		cleanAddr = strings.TrimPrefix(cleanAddr, "valkey://")
	} else if strings.HasPrefix(cleanAddr, "tcp://") {
		cleanAddr = strings.TrimPrefix(cleanAddr, "tcp://")
	}

	var pass string
	if len(password) > 0 {
		pass = password[0]
	}

	return &RespClient{
		addr:     cleanAddr,
		password: pass,
		subChan:  make(chan struct{}),
	}
}

func (c *RespClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil
	}

	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to valkey at %s: %w", c.addr, err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)

	if c.password != "" {
		if err := c.executeCommandLocked("AUTH", c.password); err != nil {
			c.conn.Close()
			c.conn = nil
			c.reader = nil
			return fmt.Errorf("valkey authentication failed: %w", err)
		}
	}

	return nil
}

func (c *RespClient) executeCommandLocked(args ...string) error {
	cmd := formatRESPCommand(args...)
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		return err
	}

	_, err := parseRESP(c.reader)
	return err
}

func (c *RespClient) executeCommand(args ...string) (any, error) {
	if err := c.connect(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := formatRESPCommand(args...)
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		c.conn.Close()
		c.conn = nil
		c.reader = nil
		return nil, err
	}

	return parseRESP(c.reader)
}

func (c *RespClient) Ping(ctx context.Context) error {
	res, err := c.executeCommand("PING")
	if err != nil {
		return err
	}
	if s, ok := res.(string); ok && strings.EqualFold(s, "PONG") {
		return nil
	}
	return nil
}

func (c *RespClient) SetRoute(ctx context.Context, hostname string, route *RouteInfo) error {
	data, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("failed to marshal route info: %w", err)
	}
	key := ValkeyRouteKeyPrefix + hostname
	_, err = c.executeCommand("SET", key, string(data))
	return err
}

func (c *RespClient) GetRoute(ctx context.Context, hostname string) (*RouteInfo, error) {
	key := ValkeyRouteKeyPrefix + hostname
	res, err := c.executeCommand("GET", key)
	if err != nil {
		return nil, err
	}
	dataStr, ok := res.(string)
	if !ok || dataStr == "" {
		return nil, errors.New("route not found")
	}

	var info RouteInfo
	if err := json.Unmarshal([]byte(dataStr), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *RespClient) DeleteRoute(ctx context.Context, hostname string) error {
	key := ValkeyRouteKeyPrefix + hostname
	_, err := c.executeCommand("DEL", key)
	return err
}

func (c *RespClient) Publish(ctx context.Context, channel string, message string) error {
	_, err := c.executeCommand("PUBLISH", channel, message)
	return err
}

func (c *RespClient) Subscribe(ctx context.Context, channel string, onMessage func(channel, message string)) error {
	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect subscription to valkey at %s: %w", c.addr, err)
	}
	c.subConn = conn
	reader := bufio.NewReader(conn)

	if c.password != "" {
		authCmd := formatRESPCommand("AUTH", c.password)
		if _, err := conn.Write([]byte(authCmd)); err != nil {
			conn.Close()
			return err
		}
		if _, err := parseRESP(reader); err != nil {
			conn.Close()
			return err
		}
	}

	subCmd := formatRESPCommand("SUBSCRIBE", channel)
	if _, err := conn.Write([]byte(subCmd)); err != nil {
		conn.Close()
		return err
	}

	go func() {
		defer conn.Close()
		for {
			select {
			case <-c.subChan:
				return
			case <-ctx.Done():
				return
			default:
				val, err := parseRESP(reader)
				if err != nil {
					return
				}
				if arr, ok := val.([]any); ok && len(arr) == 3 {
					msgType, _ := arr[0].(string)
					ch, _ := arr[1].(string)
					msg, _ := arr[2].(string)
					if strings.EqualFold(msgType, "message") && ch == channel {
						onMessage(ch, msg)
					}
				}
			}
		}
	}()

	return nil
}

func (c *RespClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.subChan:
	default:
		close(c.subChan)
	}

	if c.subConn != nil {
		c.subConn.Close()
		c.subConn = nil
	}
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.reader = nil
		return err
	}
	return nil
}

// RESP Protocol parsing and formatting helpers

func formatRESPCommand(args ...string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, arg := range args {
		sb.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}
	return sb.String()
}

func parseRESP(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(line, "\r\n")
	line = strings.TrimSuffix(line, "\n")
	if len(line) == 0 {
		return nil, errors.New("empty resp line")
	}

	prefix := line[0]
	content := line[1:]

	switch prefix {
	case '+': // Simple string
		return content, nil
	case '-': // Error
		return nil, errors.New(content)
	case ':': // Integer
		return strconv.ParseInt(content, 10, 64)
	case '$': // Bulk string
		length, err := strconv.Atoi(content)
		if err != nil {
			return nil, err
		}
		if length == -1 {
			return "", nil // nil bulk string
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		// Discard trailing CRLF
		_, _ = r.ReadString('\n')
		return string(buf), nil
	case '*': // Array
		length, err := strconv.Atoi(content)
		if err != nil {
			return nil, err
		}
		if length == -1 {
			return nil, nil
		}
		items := make([]any, length)
		for i := 0; i < length; i++ {
			item, err := parseRESP(r)
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unknown resp prefix: %c", prefix)
	}
}

// MockValkeyClient provides an in-memory client for testing or standalone execution
type MockValkeyClient struct {
	mu          sync.RWMutex
	routes      map[string]*RouteInfo
	subscribers map[string][]func(channel, message string)
	closed      bool
}

func NewMockValkeyClient() *MockValkeyClient {
	return &MockValkeyClient{
		routes:      make(map[string]*RouteInfo),
		subscribers: make(map[string][]func(channel, message string)),
	}
}

func (m *MockValkeyClient) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errors.New("client closed")
	}
	return nil
}

func (m *MockValkeyClient) SetRoute(ctx context.Context, hostname string, route *RouteInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("client closed")
	}
	m.routes[hostname] = route
	return nil
}

func (m *MockValkeyClient) GetRoute(ctx context.Context, hostname string) (*RouteInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, errors.New("client closed")
	}
	r, ok := m.routes[hostname]
	if !ok {
		return nil, errors.New("route not found")
	}
	return r, nil
}

func (m *MockValkeyClient) DeleteRoute(ctx context.Context, hostname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("client closed")
	}
	delete(m.routes, hostname)
	return nil
}

func (m *MockValkeyClient) Publish(ctx context.Context, channel string, message string) error {
	m.mu.RLock()
	callbacks := append([]func(channel, message string){}, m.subscribers[channel]...)
	m.mu.RUnlock()

	for _, cb := range callbacks {
		go cb(channel, message)
	}
	return nil
}

func (m *MockValkeyClient) Subscribe(ctx context.Context, channel string, onMessage func(channel, message string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("client closed")
	}
	m.subscribers[channel] = append(m.subscribers[channel], onMessage)
	return nil
}

func (m *MockValkeyClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.subscribers = make(map[string][]func(channel, message string))
	return nil
}

// ValkeySyncManager orchestrates routing synchronization with Valkey / Dragonfly
type ValkeySyncManager struct {
	client     ValkeyClient
	nodeID     string
	logger     *logger.Logger
	onRemoteOp func(event *RoutingEvent)
	cancel     context.CancelFunc
	ctx        context.Context
}

func NewValkeySyncManager(client ValkeyClient, nodeID string, log *logger.Logger) *ValkeySyncManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ValkeySyncManager{
		client: client,
		nodeID: nodeID,
		logger: log,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (v *ValkeySyncManager) SetRemoteHandler(handler func(event *RoutingEvent)) {
	v.onRemoteOp = handler
}

func (v *ValkeySyncManager) Start(ctx context.Context) error {
	if v.client == nil {
		return nil
	}

	return v.client.Subscribe(ctx, ValkeyChannelRoutingEvents, func(channel, message string) {
		var event RoutingEvent
		if err := json.Unmarshal([]byte(message), &event); err != nil {
			if v.logger != nil {
				v.logger.Warn("Failed to unmarshal routing sync event: %v", err)
			}
			return
		}

		// Discard events originating from this node to avoid duplicate updates
		if event.OriginNode == v.nodeID {
			return
		}

		if v.logger != nil {
			v.logger.Info("Received remote routing sync event: %s for host %s from node %s", event.Action, event.Hostname, event.OriginNode)
		}

		if v.onRemoteOp != nil {
			v.onRemoteOp(&event)
		}
	})
}

func (v *ValkeySyncManager) BroadcastRouteAdd(ctx context.Context, serverID, hostname, backendHost string, backendPort, proxyPort int) error {
	if v.client == nil {
		return nil
	}

	info := &RouteInfo{
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		ProxyPort:   proxyPort,
		UpdatedAt:   time.Now().Unix(),
	}

	// 1. Store in KV
	_ = v.client.SetRoute(ctx, hostname, info)

	// 2. Publish broadcast event
	event := &RoutingEvent{
		Action:      "add",
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		ProxyPort:   proxyPort,
		OriginNode:  v.nodeID,
		Timestamp:   time.Now().Unix(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return v.client.Publish(ctx, ValkeyChannelRoutingEvents, string(data))
}

func (v *ValkeySyncManager) BroadcastRouteRemove(ctx context.Context, hostname string, proxyPort int) error {
	if v.client == nil {
		return nil
	}

	_ = v.client.DeleteRoute(ctx, hostname)

	event := &RoutingEvent{
		Action:     "remove",
		Hostname:   hostname,
		ProxyPort:  proxyPort,
		OriginNode: v.nodeID,
		Timestamp:  time.Now().Unix(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return v.client.Publish(ctx, ValkeyChannelRoutingEvents, string(data))
}

func (v *ValkeySyncManager) Close() error {
	v.cancel()
	if v.client != nil {
		return v.client.Close()
	}
	return nil
}
