// Command healthcheck is the container liveness probe for the llama-swap
// aggregator image.
//
// The image is built FROM scratch, so the upstream image's
// `CMD-SHELL curl -f http://localhost:8080/` healthcheck is not available:
// there is no shell and no curl. This is a static, dependency-free equivalent
// that keeps parity with upstream (same interval semantics, same "2xx means
// alive" rule) and adds one aggregator-specific check: the podman socket that
// llama-swap needs in order to spawn model containers is reachable.
//
// Exit code 0 means healthy, 1 means unhealthy (podman/docker marks the
// container unhealthy after `retries` consecutive failures).
//
// Configuration (all optional):
//
//	HEALTHCHECK_URL          endpoint to probe   (default: derived, see below)
//	HEALTHCHECK_TIMEOUT      per-probe timeout   (default 3s)
//	HEALTHCHECK_SKIP_SOCKET  skip the socket probe (default false)
//
// The probe URL is resolved in this order:
//
//  1. HEALTHCHECK_URL, if set
//  2. the port llama-swap is actually listening on, read from its own
//     command line (/proc/1/cmdline — it is PID 1 in this container), so a
//     deployment that runs `-listen 0.0.0.0:10301` needs no configuration
//  3. http://127.0.0.1:8080/health, the image default
//
// Step 2 exists because a healthcheck pinned to a port the operator moved is
// worse than no healthcheck: it reports a perfectly healthy server as dead.
//
// The socket probe only runs when a unix:// host is actually configured via
// CONTAINER_HOST / PODMAN_HOST (the same precedence podman-remote itself
// uses). An image run without a socket is not treated as unhealthy: there is
// nothing declared to check.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultURL     = "http://127.0.0.1:8080/health"
	defaultTimeout = 3 * time.Second
)

type config struct {
	url        string
	timeout    time.Duration
	socketPath string // empty when no unix:// host is configured
	skipSocket bool
}

// listenPort parses a llama-swap command line (argv) and returns the port
// from -listen/--listen, accepting both `-listen X` and `-listen=X` forms.
// Returns "" when the flag is absent or the value has no port.
func listenPort(argv []string) string {
	for i, arg := range argv {
		name, val, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name != "listen" {
			continue
		}
		raw := val
		if !hasVal {
			if i+1 >= len(argv) {
				continue
			}
			raw = argv[i+1]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Accept ":8080", "0.0.0.0:10301", "[::]:10301", "localhost:8080",
		// and a bare "8080".
		if _, port, err := net.SplitHostPort(raw); err == nil {
			return port
		}
		if strings.HasPrefix(raw, ":") {
			return strings.TrimPrefix(raw, ":")
		}
		if isDigits(raw) {
			return raw
		}
		return ""
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// readArgv reads a NUL-separated argv file (/proc/<pid>/cmdline).
func readArgv(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var argv []string
	for _, part := range strings.Split(string(data), "\x00") {
		if part != "" {
			argv = append(argv, part)
		}
	}
	return argv
}

// discoverURL finds the port llama-swap listens on from PID 1's command line.
// Only trusted when PID 1 really is llama-swap; otherwise the caller falls
// back to the image default rather than probing some unrelated port.
func discoverURL() string {
	return discoverURLFrom("/proc/1/cmdline")
}

func discoverURLFrom(path string) string {
	argv := readArgv(path)
	if len(argv) == 0 || !strings.Contains(strings.ToLower(argv[0]), "llama-swap") {
		return ""
	}
	if port := listenPort(argv); port != "" {
		return "http://127.0.0.1:" + port + "/health"
	}
	return ""
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		url:     defaultURL,
		timeout: defaultTimeout,
	}
	if v := getenv("HEALTHCHECK_URL"); v != "" {
		c.url = v
	} else if discovered := discoverURL(); discovered != "" {
		c.url = discovered
	}
	if raw := getenv("HEALTHCHECK_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return c, fmt.Errorf("HEALTHCHECK_TIMEOUT %q: %w", raw, err)
		}
		c.timeout = d
	}
	c.skipSocket = truthy(getenv("HEALTHCHECK_SKIP_SOCKET"))

	// podman-remote reads CONTAINER_HOST before PODMAN_HOST; mirror that so
	// we probe the socket the client inside this container would really use.
	for _, key := range []string{"CONTAINER_HOST", "PODMAN_HOST"} {
		if v := getenv(key); strings.HasPrefix(v, "unix://") {
			c.socketPath = strings.TrimPrefix(v, "unix://")
			break
		}
	}
	return c, nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// probe runs the HTTP liveness check, then the podman socket check.
func probe(c config) error {
	client := &http.Client{Timeout: c.timeout}
	req, err := http.NewRequest(http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", c.url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("llama-swap %s: %w", c.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("llama-swap %s: HTTP %d", c.url, resp.StatusCode)
	}

	if c.skipSocket || c.socketPath == "" {
		return nil
	}
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return fmt.Errorf("podman socket %s: %w", c.socketPath, err)
	}
	return conn.Close()
}

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err == nil {
		err = probe(cfg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhealthy: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("healthy")
}
