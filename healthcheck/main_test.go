package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func envFn(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(envFn(map[string]string{}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.url != defaultURL {
		t.Errorf("url = %q, want %q", c.url, defaultURL)
	}
	if c.timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", c.timeout, defaultTimeout)
	}
	if c.socketPath != "" {
		t.Errorf("socketPath = %q, want empty with no host configured", c.socketPath)
	}
}

func TestLoadConfigSocketPrecedence(t *testing.T) {
	// CONTAINER_HOST wins over PODMAN_HOST, matching podman-remote itself.
	c, err := loadConfig(envFn(map[string]string{
		"CONTAINER_HOST": "unix:///podman.sock",
		"PODMAN_HOST":    "unix:///other.sock",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.socketPath != "/podman.sock" {
		t.Errorf("socketPath = %q, want /podman.sock", c.socketPath)
	}

	// A non-unix host (tcp://) leaves nothing local to probe.
	c, _ = loadConfig(envFn(map[string]string{"CONTAINER_HOST": "tcp://host:2375"}))
	if c.socketPath != "" {
		t.Errorf("socketPath = %q for tcp host, want empty", c.socketPath)
	}
}

func TestLoadConfigBadTimeout(t *testing.T) {
	if _, err := loadConfig(envFn(map[string]string{"HEALTHCHECK_TIMEOUT": "banana"})); err == nil {
		t.Fatal("expected error for unparseable HEALTHCHECK_TIMEOUT")
	}
}

func TestProbeHTTPOk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	if err := probe(config{url: srv.URL + "/health", timeout: 2 * time.Second}); err != nil {
		t.Fatalf("probe: %v", err)
	}
}

func TestProbeHTTPBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := probe(config{url: srv.URL + "/health", timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected error on 503")
	}
}

func TestProbeSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "podman.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()

	c := config{url: deadURL(t), timeout: 2 * time.Second, socketPath: sock}
	// HTTP is dead, so the probe must fail on HTTP before ever reaching the
	// socket: order matters, the socket is the secondary check.
	if err := probe(c); err == nil {
		t.Fatal("expected HTTP failure to dominate")
	}

	// With a live HTTP endpoint, the socket decides.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := probe(config{url: srv.URL + "/health", timeout: 2 * time.Second, socketPath: sock}); err != nil {
		t.Fatalf("live socket should pass: %v", err)
	}

	missing := filepath.Join(dir, "absent.sock")
	if err := probe(config{url: srv.URL + "/health", timeout: 2 * time.Second, socketPath: missing}); err == nil {
		t.Fatal("missing socket should fail the probe")
	}
	// ...unless explicitly skipped.
	if err := probe(config{url: srv.URL + "/health", timeout: 2 * time.Second, socketPath: missing, skipSocket: true}); err != nil {
		t.Fatalf("skipSocket should pass: %v", err)
	}
}

func TestListenPort(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"docker-style long flag", []string{"/app/llama-swap", "--config", "/app/config.yaml", "--listen", "0.0.0.0:10301", "--watch-config"}, "10301"},
		{"short flag", []string{"/app/llama-swap", "-listen", "0.0.0.0:10301"}, "10301"},
		{"equals form", []string{"/app/llama-swap", "-listen=8080"}, "8080"},
		{"long equals form", []string{"/app/llama-swap", "--listen=:9292"}, "9292"},
		{"bare colon", []string{"/app/llama-swap", "-listen", ":8080"}, "8080"},
		{"ipv6", []string{"/app/llama-swap", "-listen", "[::]:10301"}, "10301"},
		{"localhost host", []string{"/app/llama-swap", "-listen", "localhost:8080"}, "8080"},
		{"bare port value", []string{"/app/llama-swap", "-listen", "8080"}, "8080"},
		{"flag absent", []string{"/app/llama-swap", "-config", "c.yaml"}, ""},
		{"flag with no value", []string{"/app/llama-swap", "-listen"}, ""},
		{"unparseable value", []string{"/app/llama-swap", "-listen", "nonsense"}, ""},
		{"similar flag name ignored", []string{"/app/llama-swap", "-listener", "9999"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listenPort(tc.argv); got != tc.want {
				t.Errorf("listenPort(%v) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

func writeCmdline(t *testing.T, argv ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cmdline")
	var b []byte
	for _, a := range argv {
		b = append(b, []byte(a)...)
		b = append(b, 0)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDiscoverURLFrom(t *testing.T) {
	// The real deployment shape: compose passes --listen 0.0.0.0:10301.
	got := discoverURLFrom(writeCmdline(t, "/app/llama-swap", "--config", "/app/config.yaml",
		"--listen", "0.0.0.0:10301", "--watch-config"))
	if got != "http://127.0.0.1:10301/health" {
		t.Errorf("discovered %q, want http://127.0.0.1:10301/health", got)
	}

	// PID 1 is not llama-swap: do not trust its flags.
	if got := discoverURLFrom(writeCmdline(t, "/usr/bin/something", "--listen", "1.2.3.4:9")); got != "" {
		t.Errorf("discovered %q from a non-llama-swap argv, want empty", got)
	}

	// llama-swap with no -listen flag: fall back to the image default.
	if got := discoverURLFrom(writeCmdline(t, "/app/llama-swap", "-config", "/app/config.yaml")); got != "" {
		t.Errorf("discovered %q with no -listen flag, want empty", got)
	}

	// Missing file must not blow up.
	if got := discoverURLFrom(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Errorf("discovered %q from a missing file, want empty", got)
	}
}

func TestExplicitURLOverridesDiscovery(t *testing.T) {
	c, err := loadConfig(envFn(map[string]string{"HEALTHCHECK_URL": "http://example.invalid/health"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.url != "http://example.invalid/health" {
		t.Errorf("HEALTHCHECK_URL did not win: %q", c.url)
	}
}

// deadURL returns a URL whose port nothing is listening on.
func deadURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr + "/health"
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
