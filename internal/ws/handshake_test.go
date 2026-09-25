package ws

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
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
		SessionTimeout: 3600,
		JWTSecret:      "test-secret-value-0123456789",
		Local:          config.LocalConfig{Enabled: true, AllowRegistration: false},
		OIDC:           config.OIDCConfig{Enabled: false},
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
