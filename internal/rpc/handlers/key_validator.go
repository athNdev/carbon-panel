package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/athNdev/mineserver/internal/auth"
	"github.com/athNdev/mineserver/internal/rbac"
	"github.com/athNdev/mineserver/pkg/logger"
)

// ValidateKeyRequest represents incoming request payload
type ValidateKeyRequest struct {
	Provider  string `json:"provider"`  // "curseforge" or "modrinth"
	APIKey    string `json:"api_key"`   // Token or API Key
	UserAgent string `json:"user_agent"`// Custom User-Agent
}

// ValidateKeyResponse represents validation outcome
type ValidateKeyResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

// NewKeyValidatorHandler creates an HTTP handler for testing CurseForge and Modrinth API credentials
func NewKeyValidatorHandler(authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) http.Handler {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Auth check if enabled
		if authManager != nil && authManager.IsAnyAuthEnabled() {
			authHeader := r.Header.Get("Authorization")
			user, err := authManager.AuthenticateFromHeader(r.Context(), authHeader)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if enforcer != nil {
				allowed, rbacErr := enforcer.Enforce(user.Roles, rbac.ResourceSettings, rbac.ActionRead, "*")
				if rbacErr != nil || !allowed {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
		}

		var req ValidateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		resp := ValidateKeyResponse{Valid: false}

		switch strings.ToLower(strings.TrimSpace(req.Provider)) {
		case "curseforge", "fuego":
			key := strings.TrimSpace(req.APIKey)
			if key == "" {
				resp.Message = "CurseForge API key is empty"
				writeJSON(w, http.StatusOK, resp)
				return
			}

			// Validate with CurseForge API: get Minecraft mod categories
			reqCF, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.curseforge.com/v1/categories?gameId=432", nil)
			if err != nil {
				resp.Message = fmt.Sprintf("Failed to build request: %v", err)
				writeJSON(w, http.StatusOK, resp)
				return
			}
			reqCF.Header.Set("x-api-key", key)
			reqCF.Header.Set("Accept", "application/json")

			res, err := httpClient.Do(reqCF)
			if err != nil {
				resp.Message = fmt.Sprintf("Connection to CurseForge API failed: %v", err)
				writeJSON(w, http.StatusOK, resp)
				return
			}
			defer res.Body.Close()

			if res.StatusCode == http.StatusOK {
				// Also check if mods/search endpoint allows this key
				reqSearch, sErr := http.NewRequestWithContext(r.Context(), "GET", "https://api.curseforge.com/v1/mods/search?gameId=432&pageSize=1", nil)
				if sErr == nil {
					reqSearch.Header.Set("x-api-key", key)
					reqSearch.Header.Set("Accept", "application/json")
					resSearch, doErr := httpClient.Do(reqSearch)
					if doErr == nil {
						defer resSearch.Body.Close()
						if resSearch.StatusCode == http.StatusOK {
							resp.Valid = true
							resp.Message = "Connected successfully! CurseForge API key is valid and approved for mod searches."
						} else if resSearch.StatusCode == http.StatusForbidden || resSearch.StatusCode == http.StatusUnauthorized {
							resp.Valid = true
							resp.Message = "CurseForge API key accepted, but Overwolf restricts direct mod search (403). MINESERVER will automatically use the keyless community proxy for mod searches!"
						} else {
							resp.Valid = true
							resp.Message = "Connected successfully! CurseForge API key is valid."
						}
					} else {
						resp.Valid = true
						resp.Message = "Connected successfully! CurseForge API key is valid."
					}
				} else {
					resp.Valid = true
					resp.Message = "Connected successfully! CurseForge API key is valid."
				}
			} else if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized {
				resp.Message = "CurseForge rejected API key (403 Forbidden). Please verify your key at https://console.curseforge.com/#/api-keys."
			} else {
				body, _ := io.ReadAll(res.Body)
				resp.Message = fmt.Sprintf("CurseForge returned status %d: %s", res.StatusCode, string(body))
			}

		case "modrinth":
			token := strings.TrimSpace(req.APIKey)
			ua := strings.TrimSpace(req.UserAgent)
			if ua == "" {
				ua = "MineServer/1.0 (MINESERVER-admin)"
			}

			if token != "" {
				// Authenticated check with Modrinth user endpoint
				reqMR, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.modrinth.com/v2/user", nil)
				if err != nil {
					resp.Message = fmt.Sprintf("Failed to build request: %v", err)
					writeJSON(w, http.StatusOK, resp)
					return
				}
				reqMR.Header.Set("Authorization", token)
				reqMR.Header.Set("User-Agent", ua)

				res, err := httpClient.Do(reqMR)
				if err != nil {
					resp.Message = fmt.Sprintf("Connection to Modrinth failed: %v", err)
					writeJSON(w, http.StatusOK, resp)
					return
				}
				defer res.Body.Close()

				if res.StatusCode == http.StatusOK {
					var user struct {
						Username string `json:"username"`
					}
					_ = json.NewDecoder(res.Body).Decode(&user)
					resp.Valid = true
					if user.Username != "" {
						resp.Message = fmt.Sprintf("Authenticated successfully as %s on Modrinth.", user.Username)
					} else {
						resp.Message = "Authenticated successfully with Modrinth API."
					}
				} else if res.StatusCode == http.StatusUnauthorized {
					resp.Message = "Modrinth rejected Personal Access Token (401 Unauthorized)."
				} else {
					resp.Message = fmt.Sprintf("Modrinth returned status %d", res.StatusCode)
				}
			} else {
				// Anonymous connectivity check
				reqMR, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.modrinth.com/v2/search?limit=1", nil)
				if err != nil {
					resp.Message = fmt.Sprintf("Failed to build request: %v", err)
					writeJSON(w, http.StatusOK, resp)
					return
				}
				reqMR.Header.Set("User-Agent", ua)

				res, err := httpClient.Do(reqMR)
				if err != nil {
					resp.Message = fmt.Sprintf("Connection to Modrinth failed: %v", err)
					writeJSON(w, http.StatusOK, resp)
					return
				}
				defer res.Body.Close()

				if res.StatusCode == http.StatusOK {
					resp.Valid = true
					resp.Message = "Modrinth API connected successfully (public access)."
				} else {
					resp.Message = fmt.Sprintf("Modrinth returned status %d", res.StatusCode)
				}
			}

		default:
			resp.Message = fmt.Sprintf("Unsupported provider: %s", req.Provider)
		}

		writeJSON(w, http.StatusOK, resp)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
