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
