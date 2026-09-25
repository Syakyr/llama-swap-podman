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
//	HEALTHCHECK_URL          endpoint to probe   (default http://127.0.0.1:8080/health)
//	HEALTHCHECK_TIMEOUT      per-probe timeout   (default 3s)
//	HEALTHCHECK_SKIP_SOCKET  skip the socket probe (default false)
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

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		url:     defaultURL,
		timeout: defaultTimeout,
	}
	if v := getenv("HEALTHCHECK_URL"); v != "" {
		c.url = v
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
