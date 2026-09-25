package ws

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// newHandshakeTestHub builds a Hub with local auth enabled and a single
// user (username/password) that can mint real session tokens via Login.
func newHandshakeTestHub(t *testing.T, anonymousAccess bool) (*Hub, string) {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := db.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	authManager, err := auth.NewManager(store, nil, &config.AuthConfig{
		SessionTimeout:  3600,
		JWTSecret:       "test-secret-value-0123456789",
		Local:           config.LocalConfig{Enabled: true, AllowRegistration: false},
		OIDC:            config.OIDCConfig{Enabled: false},
		AnonymousAccess: anonymousAccess,
	})
	if err != nil {
		t.Fatalf("failed to create auth manager: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	if err := store.CreateUser(context.Background(), &db.User{
		ID:           "user-1",
		Username:     "tester",
		PasswordHash: string(hash),
		AuthProvider: "local",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	_, _, token, _, err := authManager.Login(context.Background(), "tester", "test-password")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	hub := NewHub(nil, authManager, nil, store, nil, nil, logger.New())
	go hub.Run()
	return hub, token
}

func TestHandshakeToken_Extraction(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws?token=abc123", nil)
	if got := handshakeToken(req); got != "abc123" {
		t.Errorf("query token = %q, want abc123", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer sess.token.here")
	if got := handshakeToken(req); got != "sess.token.here" {
		t.Errorf("bearer token = %q, want sess.token.here", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	if got := handshakeToken(req); got != "" {
		t.Errorf("empty credential = %q, want empty", got)
	}
}

// The handshake must be rejected before Upgrade when no usable credential
// is presented and anonymous access is off (MINE-164, research P2-2).
func TestServeHTTP_RejectsUnauthenticatedHandshake(t *testing.T) {
	hub, _ := newHandshakeTestHub(t, false)

	for name, mutate := range map[string]func(*http.Request){
		"no credential": func(r *http.Request) {},
		"garbage token": func(r *http.Request) {
			q := r.URL.Query()
			q.Set("token", "not-a-real-token")
			r.URL.RawQuery = q.Encode()
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			mutate(req)
			rec := httptest.NewRecorder()
			hub.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", res.StatusCode)
			}
		})
	}
}

func TestServeHTTP_UpgradesWithValidToken(t *testing.T) {
	hub, token := newHandshakeTestHub(t, false)
	ts := httptest.NewServer(hub)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + token
	conn, res, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial with valid token failed: %v (status %v)", err, res)
	}
	defer conn.Close()

	// The pre-authenticated client must be able to use the socket without
	// sending AUTH first: ping/pong proves the pumps run.
	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestServeHTTP_UpgradesWithBearerHeader(t *testing.T) {
	hub, token := newHandshakeTestHub(t, false)
	ts := httptest.NewServer(hub)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial with bearer token failed: %v", err)
	}
	defer conn.Close()
}

func TestServeHTTP_AllowsAnonymousWhenEnabled(t *testing.T) {
	hub, _ := newHandshakeTestHub(t, true)
	ts := httptest.NewServer(hub)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("anonymous dial failed: %v", err)
	}
	defer conn.Close()
}

func drainSend(t *testing.T, c *Client) [][]byte {
	t.Helper()
	var out [][]byte
	for {
		select {
		case data := <-c.send:
			out = append(out, data)
		default:
			return out
		}
	}
}

func unmarshalServerMessage(t *testing.T, data []byte) *v1.WebSocketServerMessage {
	t.Helper()
	msg := &v1.WebSocketServerMessage{}
	if err := proto.Unmarshal(data, msg); err != nil {
		t.Fatalf("failed to unmarshal server message: %v", err)
	}
	return msg
}

// Log lines dropped on a full buffer are counted and reported with a
// sentinel before the next delivered line (MINE-165, research P2-1).
func TestSendLog_DropCountingAndSentinel(t *testing.T) {
	c := &Client{hub: &Hub{log: logger.New()}, send: make(chan []byte, 2)}

	entry := func(text string) *v1.LogEntry { return &v1.LogEntry{Message: text} }

	// Fill both buffer slots, then overflow twice.
	c.sendLog("s1", entry("first"))
	c.sendLog("s1", entry("second"))
	c.sendLog("s1", entry("dropped-1"))
	c.sendLog("s1", entry("dropped-2"))
	if got := c.droppedLogs.Load(); got != 2 {
		t.Fatalf("droppedLogs = %d, want 2", got)
	}

	// Drain the buffered lines; the next sendLog must deliver the sentinel
	// first, then the fresh entry.
	got := drainSend(t, c)
	if len(got) != 2 {
		t.Fatalf("expected 2 buffered messages, got %d", len(got))
	}
	if text := unmarshalServerMessage(t, got[0]).GetLog().GetLog().GetMessage(); text != "first" {
		t.Fatalf("buffered message = %q, want first", text)
	}

	c.sendLog("s1", entry("fresh"))
	got = drainSend(t, c)
	if len(got) != 2 {
		t.Fatalf("expected sentinel + entry, got %d messages", len(got))
	}
	sentinel := unmarshalServerMessage(t, got[0]).GetLog().GetLog()
	if !strings.Contains(sentinel.GetMessage(), "2 log lines skipped") {
		t.Errorf("sentinel = %q, want skip notice for 2 lines", sentinel.GetMessage())
	}
	if sentinel.GetLevel() != "warn" {
		t.Errorf("sentinel level = %q, want warn", sentinel.GetLevel())
	}
	if text := unmarshalServerMessage(t, got[1]).GetLog().GetLog().GetMessage(); text != "fresh" {
		t.Errorf("entry after sentinel = %q, want fresh", text)
	}
	if got := c.droppedLogs.Load(); got != 0 {
		t.Errorf("droppedLogs = %d after flush, want 0", got)
	}
}

// Control messages wait for buffer space instead of dropping (MINE-165).
func TestSendControl_WaitsForBufferSpace(t *testing.T) {
	c := &Client{hub: &Hub{log: logger.New()}, send: make(chan []byte, 1)}
	c.send <- []byte("filler")

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.sendError("boom")
	}()

	select {
	case <-done:
		t.Fatal("sendControl delivered while buffer full")
	case <-time.After(50 * time.Millisecond):
	}

	<-c.send // free the slot; the blocked control message must land next
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sendControl did not deliver after buffer drained")
	}

	msg := unmarshalServerMessage(t, <-c.send)
	if msg.GetError().GetError() != "boom" {
		t.Errorf("control payload = %q, want boom", msg.GetError().GetError())
	}
}
