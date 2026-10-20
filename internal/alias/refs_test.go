// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package alias

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestNewContext_HostIDs(t *testing.T) {
	ctx := NewContext()
	if ctx == nil || ctx.Host == nil {
		t.Fatal("nil context or host")
	}
	// os.Getuid/Getgid return -1 on platforms without Unix user IDs.
	if runtime.GOOS != "windows" && (ctx.Host.UID < 0 || ctx.Host.GID < 0) {
		t.Errorf("host IDs = %d/%d", ctx.Host.UID, ctx.Host.GID)
	}
}

func TestSubstituteModuleReferences(t *testing.T) {
	modules := map[string]*models.Module{
		"mysql": {
			ID:   "mod-mysql",
			Name: "MySQL",
			Ports: []*v1.ModulePort{
				{Name: "db", ContainerPort: 3306},
			},
		},
	}

	cases := []struct {
		name  string
		in    string
		check func(string) bool
	}{
		{"no alias passthrough", "plain text", func(s string) bool { return s == "plain text" }},
		{"id field", "jdbc:{{modules.mysql.id}}", func(s string) bool { return strings.Contains(s, "mod-mysql") }},
		{"computed host", "host={{modules.mysql.host}}", func(s string) bool {
			return strings.Contains(s, "carbon-panel-module-mod-mysql")
		}},
		{"computed port", "port={{modules.mysql.port}}", func(s string) bool {
			return strings.Contains(s, "3306")
		}},
		{"unknown module stripped", "x{{modules.nope.id}}y", func(s string) bool { return s == "xy" }},
		{"unterminated left intact-ish", "x{{modules.mysql.id", func(s string) bool {
			return !strings.Contains(s, "{{modules.mysql.id}}")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := substituteModuleReferences(tc.in, modules); !tc.check(got) {
				t.Errorf("substitute(%q) = %q", tc.in, got)
			}
		})
	}

	// Nil module entries must not panic.
	out := substituteModuleReferences("{{modules.nilmod.id}}", map[string]*models.Module{"nilmod": nil})
	if strings.Contains(out, "{{") {
		t.Errorf("nil module not stripped: %q", out)
	}
}

func TestGetModuleFieldValue_Computed(t *testing.T) {
	m := &models.Module{ID: "m1", Name: "M", Ports: []*v1.ModulePort{{Name: "web", ContainerPort: 8080}}}
	if got := getModuleFieldValue(m, "host"); got != "carbon-panel-module-m1" {
		t.Errorf("host = %q", got)
	}
	if got := getModuleFieldValue(m, "port"); got != "8080" {
		t.Errorf("port = %q", got)
	}
	if got := getModuleFieldValue(&models.Module{ID: "m2"}, "port"); got != "8081" {
		t.Errorf("default port = %q", got)
	}
	if got := getModuleFieldValue(m, "nonexistent"); got != "" {
		t.Errorf("unknown field = %q", got)
	}
}

func TestGetFieldValueByJSONName(t *testing.T) {
	m := models.Module{ID: "abc", Name: "N"}
	if got := getFieldValueByJSONName(reflect.ValueOf(m), "id"); got != "abc" {
		t.Errorf("id = %q", got)
	}
}
