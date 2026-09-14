package handlers

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/athNdev/mineserver/internal/rbac"
	"github.com/athNdev/mineserver/pkg/logger"
	web "github.com/athNdev/mineserver/web/mineserver"
	"gopkg.in/yaml.v3"
)

// NewOpenAPIHandler returns an http.HandlerFunc that serves the OpenAPI spec.
// Strips Connect protocol noise and injects per-operation security overrides.
// When isAuthEnabled returns false, security schemes are removed entirely.
func NewOpenAPIHandler(log *logger.Logger, isAuthEnabled func() bool) http.HandlerFunc {
	var (
		once         sync.Once
		authEnabled  []byte
		authDisabled []byte
	)

	return func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			var raw []byte
			if buildFS, err := web.BuildFS(); err == nil {
				raw, _ = fs.ReadFile(buildFS, "schemav1.yaml")
			}
			if len(raw) == 0 {
				for _, fallbackPath := range []string{
					"web/mineserver/static/schemav1.yaml",
					"static/schemav1.yaml",
					"/workspace/mineserver/web/mineserver/static/schemav1.yaml",
				} {
					if data, err := os.ReadFile(fallbackPath); err == nil && len(data) > 0 {
						raw = data
						break
					}
				}
			}
			if len(raw) == 0 {
				log.Error("Failed to read OpenAPI spec from build or static directory")
				return
			}

			var doc map[string]any
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				log.Error("Failed to parse OpenAPI spec: %v", err)
				authEnabled = raw
				authDisabled = raw
				return
			}

			// Clean up generated spec
			if paths, ok := doc["paths"].(map[string]any); ok {
				for path, pathItem := range paths {
					methods, ok := pathItem.(map[string]any)
					if !ok {
						continue
					}
					for _, methodVal := range methods {
						op, ok := methodVal.(map[string]any)
						if !ok {
							continue
						}
						// Strip Connect-* header parameters
						if params, ok := op["parameters"].([]any); ok {
							filtered := params[:0]
							for _, p := range params {
								pm, ok := p.(map[string]any)
								if !ok {
									filtered = append(filtered, p)
									continue
								}
								name, _ := pm["name"].(string)
								if name == "Connect-Protocol-Version" || name == "Connect-Timeout-Ms" {
									continue
								}
								filtered = append(filtered, p)
							}
							if len(filtered) == 0 {
								delete(op, "parameters")
							} else {
								op["parameters"] = filtered
							}
						}
						// Mark public operations as no-auth
						procedure := "/" + strings.TrimPrefix(path, "/")
						if rbac.PublicProcedures[procedure] {
							op["security"] = []any{}
						}
					}
				}
			}

			// Remove Connect-* schema definitions
			if components, ok := doc["components"].(map[string]any); ok {
				if schemas, ok := components["schemas"].(map[string]any); ok {
					delete(schemas, "connect-protocol-version")
					delete(schemas, "connect-timeout-header")
				}
			}

			enabled, err := yaml.Marshal(doc)
			if err != nil {
				log.Error("Failed to marshal auth-enabled OpenAPI spec: %v", err)
				authEnabled = raw
			} else {
				authEnabled = enabled
			}

			// Build auth-disabled variant: strip all security fields
			delete(doc, "security")

			if components, ok := doc["components"].(map[string]any); ok {
				delete(components, "securitySchemes")
			}

			if paths, ok := doc["paths"].(map[string]any); ok {
				for _, pathItem := range paths {
					if methods, ok := pathItem.(map[string]any); ok {
						for _, methodVal := range methods {
							if op, ok := methodVal.(map[string]any); ok {
								delete(op, "security")
							}
						}
					}
				}
			}

			stripped, err := yaml.Marshal(doc)
			if err != nil {
				log.Error("Failed to marshal stripped OpenAPI spec: %v", err)
				authDisabled = raw
			} else {
				authDisabled = stripped
			}
		})

		var spec []byte
		if isAuthEnabled() {
			spec = authEnabled
		} else {
			spec = authDisabled
		}

		if spec == nil {
			http.Error(w, "OpenAPI spec not available", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(spec)
	}
}
