package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{cfgPath: filepath.Join(dir, "config.json")}
}

func TestConfigInitCmd_Run(t *testing.T) {
	app := setupTestApp(t)
	p := app.CfgPath()

	out := captureStdout(t, func() {
		if err := (&ConfigInitCmd{}).Run(app); err != nil {
			t.Fatalf("unexpected error on init: %v", err)
		}
	})
	if !strings.Contains(out, "created at") {
		t.Errorf("expected success message, got: %s", out)
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Fatal("expected config file to be created on disk")
	}

	if err := (&ConfigInitCmd{}).Run(app); err == nil {
		t.Fatal("expected error when initializing over existing file without Overwrite=true")
	}

	if err := (&ConfigInitCmd{Overwrite: true}).Run(app); err != nil {
		t.Fatalf("unexpected error with Overwrite=true: %v", err)
	}
}

func TestConfigShowCmd_Missing(t *testing.T) {
	app := setupTestApp(t)
	out := captureStdout(t, func() {
		_ = (&ConfigShowCmd{}).Run(app)
	})
	if !strings.Contains(out, "(does not exist)") {
		t.Errorf("expected '(does not exist)', got: %s", out)
	}
}

func TestConfigSetCmd_Types(t *testing.T) {
	app := setupTestApp(t)
	p := app.CfgPath()

	tests := []struct {
		key      string
		valIn    string
		expected any
	}{
		{"subdomain", "acme", "acme"},
		{"itildesk-session", "abc", "abc"},
		{"csrf-token", "tok", "tok"},
		{"enabled", "true", true},
		{"count", "42", float64(42)},
	}

	for _, tc := range tests {
		if err := (&ConfigSetCmd{Key: tc.key, Value: tc.valIn}).Run(app); err != nil {
			t.Fatalf("failed to set %s: %v", tc.key, err)
		}
	}

	m, err := loadConfigMap(p)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	for _, tc := range tests {
		got, ok := m[tc.key]
		if !ok {
			t.Errorf("expected key %q to be set", tc.key)
			continue
		}
		if got != tc.expected {
			t.Errorf("key %q: expected %v, got %v", tc.key, tc.expected, got)
		}
	}
}

func TestConfigSetCmd_Nested(t *testing.T) {
	app := setupTestApp(t)
	p := app.CfgPath()

	if err := (&ConfigSetCmd{Key: "http.timeout", Value: "30"}).Run(app); err != nil {
		t.Fatalf("failed to set nested key: %v", err)
	}

	var m map[string]any
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	sub, ok := m["http"].(map[string]any)
	if !ok || sub["timeout"] != float64(30) {
		t.Errorf("expected nested http.timeout=30, got %v", m)
	}
}

func TestConfigUnsetCmd(t *testing.T) {
	app := setupTestApp(t)
	p := app.CfgPath()

	_ = (&ConfigSetCmd{Key: "subdomain", Value: "acme"}).Run(app)
	_ = (&ConfigSetCmd{Key: "http.timeout", Value: "30"}).Run(app)

	if err := (&ConfigUnsetCmd{Key: "subdomain"}).Run(app); err != nil {
		t.Fatalf("unexpected error on unset: %v", err)
	}
	if err := (&ConfigUnsetCmd{Key: "http.timeout"}).Run(app); err != nil {
		t.Fatalf("unexpected error on nested unset: %v", err)
	}

	m, err := loadConfigMap(p)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if _, ok := m["subdomain"]; ok {
		t.Error("expected subdomain to be removed")
	}
	if _, ok := m["http"]; ok {
		t.Error("expected empty nested http object to be pruned")
	}
}
