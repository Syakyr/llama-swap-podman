<div align="center">

# 🦙 llama-swap + Podman

### llama-swap as a pure **aggregator** — its models run as containers on your host podman.

The upstream llama-swap release binary plus a static **podman-remote** CLI,
on `scratch`. No llama.cpp, no OS packages: llama-swap routes, and every
model runtime is a separate container spawned through your user podman
socket (`CONTAINER_HOST=unix:///podman.sock`).

[![build](https://github.com/syakyr/llama-swap-podman/actions/workflows/build.yml/badge.svg)](https://github.com/syakyr/llama-swap-podman/actions/workflows/build.yml)
[![watch-llama-swap](https://github.com/syakyr/llama-swap-podman/actions/workflows/watch-llama-swap.yml/badge.svg)](https://github.com/syakyr/llama-swap-podman/actions/workflows/watch-llama-swap.yml)
![latest build](https://img.shields.io/github/v/tag/Syakyr/llama-swap-podman?label=latest%20build&color=brightgreen)
![image size](https://img.shields.io/badge/image-~50_MB-brightgreen)

**Pull · Mount the socket · Done.**

</div>

---

## Quick start

```bash
# one-time: enable the user podman socket
systemctl --user start podman.socket

podman run -d --name llama-swap \
  --network host \
  -e CONTAINER_HOST=unix:///podman.sock \
  -v /run/user/$(id -u)/podman/podman.sock:/podman.sock \
  -v ./config.yaml:/app/config.yaml \
  -v ./llama-swap-data:/data/llama-swap \
  ghcr.io/syakyr/llama-swap-podman:latest
```

Or with the included compose file:

```bash
podman compose up -d
```

Confirm the baked-in CLI reaches your host daemon:

```bash
podman exec llama-swap podman info --format '{{.Host.RemoteSocket.Path}}'
```

Paths are fixed at `/app/llama-swap` (binary), `/app/config.yaml` (config)
and `/app` (working directory). In your model config, `cmd:` is a plain
`podman run …` and `cmdStop:` is `podman stop ${MODEL_ID}`.

## Images & tags

`ghcr.io/syakyr/llama-swap-podman`

| Tag | Meaning |
|---|---|
| `latest` | newest llama-swap release with a built image |
| `v<NNN>` | latest build for that llama-swap version |
| `v<NNN>-podman<X.Y.Z>` | immutable pairing of llama-swap `v<NNN>` with podman-remote `X.Y.Z` — pin this |
| `dev` | rolling build from `main`/`feat/**` |
| `dev-<shortsha>` | the exact dev build for a commit |

A watcher checks both upstreams every 6 hours and builds whenever either
moves. See [docs/releasing.md](docs/releasing.md).

## Healthcheck

The image ships a healthcheck, so `podman inspect` reports real liveness:

```bash
podman inspect -f '{{.State.Health.Status}}' llama-swap
```

It probes `GET /health` and, when `CONTAINER_HOST` declares a `unix://`
socket, that the socket is reachable. The port is discovered from the
running process, so a custom `--listen` needs no configuration. Full
reference: [docs/healthcheck.md](docs/healthcheck.md).

## Docs

| | |
|---|---|
| [docs/design.md](docs/design.md) | why a scratch repackage instead of the `cpu`/`unified` images |
| [docs/healthcheck.md](docs/healthcheck.md) | what the probe checks, port discovery, env vars, troubleshooting |
| [docs/building.md](docs/building.md) | checksum pinning, local build, smoke tests, size budget |
| [docs/releasing.md](docs/releasing.md) | tag flow, the watcher, dispatch modes, rollback |

## Layout

```
Dockerfile                 scratch build: fetch+verify, healthcheck build, runtime
healthcheck/               static Go liveness probe (unit tested)
scripts/resolve_release.py resolve upstream versions + published checksums
scripts/smoke_test.sh      the smoke suite CI runs against the built image
compose.yml                local run/deploy with the same pins
licenses/                  vendored upstream license texts
LICENSE  NOTICE            MIT (this project) + third-party inventory
docs/                      design, healthcheck, building, releasing
```

## License

MIT — see [LICENSE](LICENSE). The image ships unmodified upstream binaries
under MIT (llama-swap) and Apache-2.0 (podman-remote); the `NOTICE` and
those license texts are copied into the image at
`/usr/share/licenses/llama-swap-podman/`, so the distributed artifact
carries its own attribution. Model containers this aggregator spawns are
separate works under their own licenses. Full inventory: [NOTICE](NOTICE).

## Related

- [llama-swap](https://github.com/Mostlygeek/llama-swap) — upstream; its docs cover container-based model config
- [halogen-timings-sidecar](https://github.com/Syakyr/halogen-timings-sidecar) — one of the model runtimes this aggregator spawns
