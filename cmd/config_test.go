package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

// Every root flag must be registered in configClasses (which decides masking and
// which keys 'config set' accepts), and the secret:"" declaration must agree
// with it, so a new setting cannot be added without classifying it.
func TestConfigClassesMatchCLIFlags(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	cliValue := reflect.ValueOf(&cli).Elem()

	seen := map[string]bool{}
	for _, flag := range parser.Model.Node.Flags {
		// config-file is how the file is located and help is kong's own flag;
		// neither is a setting to store.
		if flag.Name == "config-file" || flag.Name == "help" {
			continue
		}
		seen[flag.Name] = true

		masked, registered := configClasses[flag.Name]
		if !registered {
			t.Errorf("flag %q is not registered in configClasses, so nothing decides whether it is a secret", flag.Name)
			continue
		}
		switch tagged := hasSecretTag(t, cliValue, flag); {
		case tagged && !masked:
			t.Errorf("flag %q is declared secret:\"\" but configClasses does not mask it", flag.Name)
		case !tagged && masked:
			t.Errorf("flag %q is masked by configClasses but is not declared secret:\"\"", flag.Name)
		}
	}

	for key := range configClasses {
		if !seen[key] {
			t.Errorf("configClasses contains %q, which is not a root CLI flag", key)
		}
	}
}

// hasSecretTag reports whether the CLI field kong bound flag to carries a
// secret:"" tag. The two are joined by field address, the only link kong
// exposes; if that ever stops holding this fails loudly rather than silently
// treating a credential as printable.
func hasSecretTag(t *testing.T, cliValue reflect.Value, flag *kong.Flag) bool {
	t.Helper()
	if !flag.Target.IsValid() || !flag.Target.CanAddr() {
		t.Fatalf("flag %q has no addressable target, so its secret tag cannot be read", flag.Name)
	}
	want := flag.Target.Addr().Pointer()
	cliType := cliValue.Type()
	for i := 0; i < cliType.NumField(); i++ {
		field := cliValue.Field(i)
		if field.CanAddr() && field.Addr().Pointer() == want {
			_, tagged := cliType.Field(i).Tag.Lookup("secret")
			return tagged
		}
	}
	t.Fatalf("flag %q does not correspond to any field of %s", flag.Name, cliType)
	return false
}

func TestSaveConfigMapCreatesPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "fsvc.json")

	if err := saveConfigMap(path, map[string]any{"itildesk-session": "cookie"}); err != nil {
		t.Fatalf("saveConfigMap: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected the config file to be 0600, got %04o", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("expected the config directory to be 0700, got %04o", perm)
	}
}
