package ws

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newHandshakeHub(t *testing.T, cfg *config.AuthConfig) *Hub {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	mgr, err := auth.NewManager(store, nil, cfg)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return &Hub{authManager: mgr, log: logger.New(), clients: make(map[*Client]bool)}
}

func TestAuthorizeHandshakeNoAuthOptIn(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local:       config.LocalConfig{Enabled: false},
		AllowNoAuth: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	if !h.authorizeHandshake(req) {
		t.Fatal("no-auth opt-in must authorize")
	}
}

func TestAuthorizeHandshakeNoAuthFailClosed(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local:       config.LocalConfig{Enabled: false},
		AllowNoAuth: false,
	})
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	if h.authorizeHandshake(req) {
		t.Fatal("no-auth without opt-in must reject")
	}
}

func TestAuthorizeHandshakeLocalRequiresToken(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local: config.LocalConfig{Enabled: true},
	})
	if h.authorizeHandshake(httptest.NewRequest(http.MethodGet, "/ws", nil)) {
		t.Fatal("tokenless handshake must reject when local auth is on")
	}
	bad := httptest.NewRequest(http.MethodGet, "/ws?token=junk", nil)
	if h.authorizeHandshake(bad) {
		t.Fatal("garbage token must reject")
	}
	bearer := httptest.NewRequest(http.MethodGet, "/ws", nil)
	bearer.Header.Set("Authorization", "Bearer junk")
	if h.authorizeHandshake(bearer) {
		t.Fatal("garbage bearer must reject")
	}
}

func TestAuthorizeHandshakeAnonymous(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local:           config.LocalConfig{Enabled: true},
		AnonymousAccess: true,
	})
	if !h.authorizeHandshake(httptest.NewRequest(http.MethodGet, "/ws", nil)) {
		t.Fatal("anonymous policy must authorize tokenless handshake")
	}
}

func TestServeHTTPRejectsUnauthenticatedWith401(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local: config.LocalConfig{Enabled: true},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestServeHTTPPassesAuthorizedToUpgrade(t *testing.T) {
	h := newHandshakeHub(t, &config.AuthConfig{
		Local:       config.LocalConfig{Enabled: false},
		AllowNoAuth: true,
	})
	rec := httptest.NewRecorder()
	// Plain GET is not a valid WS upgrade, so the upgrader answers 400 —
	// which proves the request passed the 401 auth gate.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (auth passed, upgrade refused)", rec.Code)
	}
}
