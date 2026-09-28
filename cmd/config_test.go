package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func setupTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{cfgPath: filepath.Join(dir, "config.json")}
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

func TestConfigShowCmd_RedactsSecrets(t *testing.T) {
	app := setupTestApp(t)

	for _, kv := range [][2]string{
		{"subdomain", "acme"},
		{"time-zone", "Europe/London"},
		{"itildesk-session", "super-secret-cookie"},
		{"csrf-token", "super-secret-csrf"},
	} {
		if err := (&ConfigSetCmd{Key: kv[0], Value: kv[1]}).Run(app); err != nil {
			t.Fatalf("failed to set %s: %v", kv[0], err)
		}
	}

	var err error
	out := captureStdout(t, func() { err = (&ConfigShowCmd{}).Run(app) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(out, "super-secret-cookie") {
		t.Error("the session cookie must never be printed")
	}
	if strings.Contains(out, "super-secret-csrf") {
		t.Error("the CSRF token must never be printed")
	}
	if strings.Count(out, "<set>") != 2 {
		t.Errorf("expected both secrets replaced with <set>, got:\n%s", out)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "Europe/London") {
		t.Errorf("expected non-secret values to be printed, got:\n%s", out)
	}

	var shown map[string]any
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("redacted output should stay valid JSON: %v\n%s", err, out)
	}
	if shown["subdomain"] != "acme" {
		t.Errorf("expected subdomain in the shown config, got %v", shown)
	}
	if shown["itildesk-session"] != "<set>" || shown["csrf-token"] != "<set>" {
		t.Errorf("expected <set> markers, got %v", shown)
	}
}

// A file that cannot be parsed must not be dumped verbatim: it may hold a
// secret that the redactor never got a chance to see.
func TestConfigShowCmd_RejectsUnparseableFile(t *testing.T) {
	app := setupTestApp(t)
	if err := os.WriteFile(app.CfgPath(), []byte("{ itildesk-session: leaked-cookie\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var err error
	out := captureStdout(t, func() { err = (&ConfigShowCmd{}).Run(app) })
	if err == nil {
		t.Fatal("expected an error for an unparseable configuration file")
	}
	if strings.Contains(out, "leaked-cookie") {
		t.Errorf("unparseable content must not be printed, got:\n%s", out)
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
		{"concurrency", "4", float64(4)},
		{"time-zone", "Europe/London", "Europe/London"},
		{"base-url", "https://acme.example", "https://acme.example"},
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

func TestConfigSetCmd_RejectsUnknownKey(t *testing.T) {
	app := setupTestApp(t)
	if err := (&ConfigSetCmd{Key: "subdomain", Value: "acme"}).Run(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The underscore is a plausible typo for itildesk-session; storing it
	// silently would leave the CLI unauthenticated with no explanation.
	err := (&ConfigSetCmd{Key: "itildesk_session", Value: "x"}).Run(app)
	if err == nil {
		t.Fatal("expected an error for an unknown key")
	}
	for _, want := range []string{"itildesk_session", "itildesk-session", "subdomain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}

	m, err := loadConfigMap(app.CfgPath())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if _, ok := m["itildesk_session"]; ok {
		t.Error("expected the unknown key not to be written")
	}
}

func TestConfigSetCmd_RejectsDottedKey(t *testing.T) {
	app := setupTestApp(t)
	if err := (&ConfigSetCmd{Key: "http.timeout", Value: "30"}).Run(app); err == nil {
		t.Fatal("expected an error for a dotted key")
	}
}

func TestConfigSetCmd_MasksSecretInOutput(t *testing.T) {
	app := setupTestApp(t)

	var err error
	out := captureStdout(t, func() {
		err = (&ConfigSetCmd{Key: "itildesk-session", Value: "super-secret-cookie"}).Run(app)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "super-secret-cookie") {
		t.Errorf("the session cookie must not be echoed, got:\n%s", out)
	}
	if !strings.Contains(out, "<set>") {
		t.Errorf("expected a <set> marker, got:\n%s", out)
	}

	// Non-secret values stay visible: they are what the user needs to confirm.
	out = captureStdout(t, func() {
		err = (&ConfigSetCmd{Key: "subdomain", Value: "acme"}).Run(app)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `= acme`) {
		t.Errorf("expected the subdomain to be echoed, got:\n%s", out)
	}
}

func TestConfigUnsetCmd(t *testing.T) {
	app := setupTestApp(t)

	_ = (&ConfigSetCmd{Key: "subdomain", Value: "acme"}).Run(app)
	_ = (&ConfigSetCmd{Key: "csrf-token", Value: "tok"}).Run(app)

	if err := (&ConfigUnsetCmd{Key: "subdomain"}).Run(app); err != nil {
		t.Fatalf("unexpected error on unset: %v", err)
	}

	m, err := loadConfigMap(app.CfgPath())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if _, ok := m["subdomain"]; ok {
		t.Error("expected subdomain to be removed")
	}
	if m["csrf-token"] != "tok" {
		t.Error("expected the other keys to survive")
	}
}

func TestConfigUnsetCmd_UnknownKey(t *testing.T) {
	app := setupTestApp(t)
	if err := (&ConfigUnsetCmd{Key: "itildesk_session"}).Run(app); err == nil {
		t.Fatal("expected an error for an unknown key")
	}
}

func TestConfigUnsetCmd_KnownKeyAbsent(t *testing.T) {
	app := setupTestApp(t)

	var err error
	out := captureStdout(t, func() { err = (&ConfigUnsetCmd{Key: "subdomain"}).Run(app) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected a 'not found' note, got:\n%s", out)
	}
}

func TestConfigPathCmd(t *testing.T) {
	app := setupTestApp(t)

	out := captureStdout(t, func() {
		if err := (&ConfigPathCmd{}).Run(app); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, app.CfgPath()) || !strings.Contains(out, "(does not exist)") {
		t.Errorf("expected the missing path to be reported, got:\n%s", out)
	}

	if err := os.WriteFile(app.CfgPath(), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out = captureStdout(t, func() {
		if err := (&ConfigPathCmd{}).Run(app); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.TrimSpace(out) != app.CfgPath() {
		t.Errorf("expected exactly the path, got %q", out)
	}
}

// configKeys is what 'config set' accepts, so it must not drift from the flags
// the CLI actually reads.
func TestConfigKeysMatchCLIFlags(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}

	// Only the root flags are configuration; subcommand flags such as
	// --older-than-days are per-invocation, not stored settings.
	flags := map[string]bool{}
	for _, f := range parser.Model.Node.Flags {
		flags[f.Name] = true
	}

	// config-file is how the file is located and help is generated by kong;
	// neither is a setting to persist.
	delete(flags, "config-file")
	delete(flags, "help")

	if len(flags) != len(configKeys) {
		t.Fatalf("expected %d config keys, found %d CLI flags: %v", len(configKeys), len(flags), flags)
	}
	for _, key := range configKeys {
		if !flags[key] {
			t.Errorf("config key %q is not a CLI flag", key)
		}
	}
}
