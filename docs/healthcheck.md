# Healthcheck

The image is `scratch` — no shell, no `curl` — so the upstream image's
`CMD-SHELL curl -f http://localhost:8080/` cannot work here. The probe is a
static Go binary (`healthcheck/`, unit tested) that fills the same role and
adds one aggregator-specific check.

## What it checks

1. **llama-swap answers** — `GET /health`, upstream's own endpoint. Any 2xx
   is healthy.
2. **the podman socket is reachable** — only when `CONTAINER_HOST` or
   `PODMAN_HOST` declares a `unix://` host. An aggregator that cannot reach
   the socket cannot spawn a model, so a live HTTP port alone is not health.

The socket check stays silent when no `unix://` host is declared: running the
image without a socket is not treated as unhealthy, because nothing was
declared to check.

## Resolving the probe URL

In order:

1. `HEALTHCHECK_URL`, if set
2. **the port llama-swap is actually listening on**, read from its own
   command line (`/proc/1/cmdline` — it is PID 1 in this container)
3. `http://127.0.0.1:8080/health`, the image default

Step 2 exists because a probe pinned to a port the operator moved is worse
than no probe: it reports a perfectly healthy server as dead. Discovery
accepts `-listen X`, `-listen=X`, `:8080`, `0.0.0.0:PORT`, `[::]:PORT` and
a bare port, and only trusts PID 1 when it is actually llama-swap — if PID 1
is something else, it falls back to the default rather than probing an
unrelated port.

## Environment

| variable | default | meaning |
|---|---|---|
| `HEALTHCHECK_URL` | *derived* | full endpoint to probe; overrides discovery |
| `HEALTHCHECK_TIMEOUT` | `3s` | per-probe timeout (HTTP and socket dial) |
| `HEALTHCHECK_SKIP_SOCKET` | `false` | skip the socket check |

Booleans accept `1`/`true`/`yes`/`on`. Timeouts are Go durations (`3s`,
`1500ms`).

Image defaults: `--interval=30s --timeout=5s --start-period=15s
--retries=3`.

## Checking status

```bash
podman inspect -f '{{.State.Health.Status}}' llama-swap
```

## Troubleshooting

The probe names the failing check, which is the fastest diagnostic:

```bash
docker exec llama-swap /app/healthcheck
```

| message | cause |
|---|---|
| `llama-swap http://…/health: connection refused` | llama-swap is not listening where the probe looks — check the `--listen` value, or set `HEALTHCHECK_URL` |
| `llama-swap http://…/health: HTTP 503` | it is listening but not serving; check the container logs |
| `podman socket /podman.sock: no such file or directory` | the socket is not mounted at the path `CONTAINER_HOST` names |
| `podman socket /podman.sock: permission denied` | mounted, but the container user cannot read it — check the host socket's ownership and mode |
| `HEALTHCHECK_TIMEOUT "banana": …` | unparseable timeout; the probe exits before checking anything |

If a check is wrong for your setup, override it rather than disabling the
healthcheck wholesale — a probe that always returns 0 is worse than no probe.
