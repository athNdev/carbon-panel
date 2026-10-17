package alias

import (
	"reflect"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	models "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestExtractTemplateVariables(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string", "", nil},
		{"no variables", "plain text without vars", nil},
		{"single variable", "host={{server.id}}", []string{"server.id"}},
		{"variable with whitespace", "url=http://{{  server.host  }}:{{ server.port }}", []string{"server.host", "server.port"}},
		{"duplicate variables deduplicated preserving order", "{{server.id}} and {{server.port}} then {{server.id}}", []string{"server.id", "server.port"}},
		{"module variable", "{{modules.db.host}}", []string{"modules.db.host"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTemplateVariables(tc.input)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("ExtractTemplateVariables(%q) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestHasUnresolvedVariables(t *testing.T) {
	if HasUnresolvedVariables("plain text") {
		t.Error("expected false for text without variables")
	}
	if !HasUnresolvedVariables("prefix {{server.id}} suffix") {
		t.Error("expected true for text with variable")
	}
	if !HasUnresolvedVariables("{{   server.port   }}") {
		t.Error("expected true for text with spaced variable")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"valid single variable", "http://{{server.host}}:{{server.port}}", false, ""},
		{"valid module reference", "{{modules.mysql.host}}", false, ""},
		{"valid host reference", "{{host.uid}}", false, ""},
		{"valid config reference", "{{config.server.port}}", false, ""},
		{"valid whitespace inside braces", "{{  server.id  }}", false, ""},
		{"unbalanced extra open", "{{server.id", true, "unbalanced"},
		{"unbalanced extra close", "server.id}}", true, "unbalanced"},
		{"empty variable", "{{}}", true, "empty"},
		{"whitespace-only variable", "{{   }}", true, "empty"},
		{"nested delimiters", "{{server.{{nested}}.id}}", true, "nested"},
		{"invalid prefix", "{{database.host}}", true, "invalid template variable prefix"},
		{"invalid variable path format", "{{server..id}}", true, "invalid template variable path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars, err := ValidateTemplate(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil (vars: %v)", tt.input, vars)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for input %q: %v", tt.input, err)
				}
			}
		})
	}
}

func TestValidateTemplateVariables_Resolution(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Port = "8080"

	server := &models.Server{
		ID:   "srv-123",
		Name: "Minecraft Server",
		Port: 25565,
	}

	modules := map[string]*models.Module{
		"mysql": {
			ID:   "mod-mysql-1",
			Name: "MySQL",
			Ports: []*v1.ModulePort{
				{Name: "db", ContainerPort: 3306},
			},
		},
	}

	ctx := &Context{
		Config:  cfg,
		Server:  server,
		Modules: modules,
	}

	// 1. All variables resolved
	input := "connect to {{modules.mysql.host}}:{{modules.mysql.port}} for {{server.id}}"
	vars, err := ValidateTemplateVariables(input, ctx)
	if err != nil {
		t.Fatalf("expected all variables to resolve, got error: %v", err)
	}
	if len(vars) != 3 {
		t.Errorf("expected 3 extracted variables, got %d", len(vars))
	}

	// 2. Unresolved variable
	unresolvedInput := "connect to {{modules.postgres.host}}:5432"
	_, err = ValidateTemplateVariables(unresolvedInput, ctx)
	if err == nil {
		t.Fatalf("expected error for unresolved module variable, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved") {
		t.Errorf("expected unresolved error, got: %v", err)
	}
}

func TestSubstitute_WhitespaceHandling(t *testing.T) {
	server := &models.Server{
		ID:   "srv-abc",
		Port: 25565,
	}
	ctx := &Context{
		Server: server,
	}

	out := Substitute("id={{  server.id  }} port={{ server.port }}", ctx)
	if !strings.Contains(out, "id=srv-abc") || !strings.Contains(out, "port=25565") {
		t.Errorf("expected whitespace-trimmed substitution, got: %q", out)
	}
}
