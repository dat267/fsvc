package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fsvcBin is the release-style binary built once in TestMain. E2E tests run
// it as a subprocess against a mock API, exercising the full path a user
// hits: process args -> Kong -> config/env resolution -> client -> output.
var (
	fsvcBin    string
	moduleRoot string
)

func TestMain(m *testing.M) {
	// e2e_test.go lives at the module root.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "e2e: cannot locate test file")
		os.Exit(1)
	}
	moduleRoot = filepath.Dir(thisFile)

	tmp, err := os.MkdirTemp("", "fsvc-e2e")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "fsvc"+exeSuffix())
	build, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e build failed: %v\n%s\n", err, build)
		os.Exit(1)
	}
	fsvcBin = bin

	os.Exit(m.Run())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// runFsvc executes the built binary with args and returns stdout+stderr and
// the exit code.
func runFsvc(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	// Drop any FSVC_* variables from the caller's environment so a developer's
	// own configuration cannot change what a test exercises.
	inherited := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "FSVC_") {
			continue
		}
		inherited = append(inherited, kv)
	}

	cmd := exec.Command(fsvcBin, args...)
	cmd.Env = append(inherited, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running fsvc: %v", err)
	}
	return string(out), code
}

// newMockAPI serves the ticket endpoints the CLI talks to.
func newMockAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") == "1" && r.URL.Query().Get("order_by") == "" {
			// session probe
			_, _ = fmt.Fprint(w, `{"tickets":[],"meta":{"count":7}}`)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"tickets":[{"id":10100,"subject":"Printer not working","status":2,"priority":2,"responder_id":3100,"created_at":"2026-08-01T10:00:00Z"}],"meta":{"has_next":false}}`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestE2E_VersionLdflags pins the release contract: -X main.version must be
// what `fsvc version` prints.
func TestE2E_VersionLdflags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fsvc"+exeSuffix())
	build, err := exec.Command("go", "build", "-ldflags=-X main.version=v9.9.9-test", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, build)
	}
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("version: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "v9.9.9-test" {
		t.Errorf("expected ldflags version, got %q", got)
	}
}

// TestE2E_SessionEnvConfig drives auth purely through documented env vars.
func TestE2E_SessionEnvConfig(t *testing.T) {
	srv := newMockAPI(t)
	out, code := runFsvc(t, []string{
		"FSVC_BASE_URL=" + srv.URL,
		"FSVC_ITILDESK_SESSION=cookie-value",
	}, "session")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "OK: authenticated") || !strings.Contains(out, "7") {
		t.Errorf("unexpected session output:\n%s", out)
	}
}

func TestE2E_MissingSession(t *testing.T) {
	srv := newMockAPI(t)
	out, code := runFsvc(t, []string{"FSVC_BASE_URL=" + srv.URL}, "session")
	if code == 0 {
		t.Fatalf("expected nonzero exit, got 0:\n%s", out)
	}
	if !strings.Contains(out, "itildesk-session") {
		t.Errorf("expected hint naming the missing config, got:\n%s", out)
	}
}

// TestE2E_SessionFileConfig drives auth purely through the config file, which
// is the path the README documents.
func TestE2E_SessionFileConfig(t *testing.T) {
	srv := newMockAPI(t)
	cfgPath := filepath.Join(t.TempDir(), "fsvc.json")
	cfg := fmt.Sprintf("{\n  \"base-url\": %q,\n  \"itildesk-session\": \"cookie-value\"\n}\n", srv.URL)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, code := runFsvc(t, []string{"FSVC_CONFIG_FILE=" + cfgPath}, "session")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "OK: authenticated") {
		t.Errorf("unexpected session output:\n%s", out)
	}
}

// runFsvc must not inherit the caller's FSVC_* variables: a developer with
// FSVC_CONFIG_FILE or FSVC_SUBDOMAIN exported would otherwise change what these
// tests exercise.
func TestE2E_AmbientConfigEnvIsStripped(t *testing.T) {
	ambient := filepath.Join(t.TempDir(), "ambient.json")
	t.Setenv("FSVC_CONFIG_FILE", ambient)

	out, code := runFsvc(t, nil, "config", "path")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if strings.Contains(out, ambient) {
		t.Errorf("ambient FSVC_CONFIG_FILE leaked into the child process:\n%s", out)
	}
}
