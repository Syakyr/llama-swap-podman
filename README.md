# llama-swap-podman

[llama-swap](https://github.com/Mostlygeek/llama-swap) (CPU image) with a
static **podman-remote** CLI baked in, so the container can drive the host's
podman socket (`CONTAINER_HOST=unix:///podman.sock`) — llama-swap spawns
model containers on the host podman instead of inside itself.

Modeled on `halogen-timings-sidecar`: pinned base images, tag-driven
provenance, GitHub Actions publishing to GHCR.

## Layout

| File | Purpose |
|---|---|
| `Dockerfile` | `ARG LLAMA_SWAP_IMAGE` (pinned `v<ver>-cpu-b<build>`) + pinned `podman-remote-static` install |
| `compose.yml` | Local run/deploy; builds the same Dockerfile, host network, podman socket mounted at `/podman.sock` |
| `scripts/resolve_base.py` | `uv run scripts/resolve_base.py v256` → newest immutable `v256-cpu-b*` tag on GHCR |
| `.github/workflows/build.yml` | Builds + smoke-tests + pushes the wrapped image |
| `.github/workflows/watch-llama-swap.yml` | Cron: new upstream release → provenance tag → dispatch `build.yml` |

## Tag scheme

Push a tag `v<llama-swap>-podman<N>` (e.g. `v256-podman1`) and `build.yml`
publishes:

- `ghcr.io/syakyr/llama-swap-podman:v256-podman1` — immutable build record
- `ghcr.io/syakyr/llama-swap-podman:v256` — latest podman build of that llama-swap version
- `ghcr.io/syakyr/llama-swap-podman:latest` — only when `v256` is the newest upstream release

The exact base (`v256-cpu-b<build>`) is resolved at build time via
`scripts/resolve_base.py` and recorded in the run summary — the floating
`cpu` tag is never baked in blindly. `workflow_dispatch` on `build.yml`
takes explicit inputs (`llama_swap_version`, `podman_version`,
`podman_build_number`, optional `base_tag` override).

## Local build / run

```sh
# prerequisites: user podman socket
systemctl --user start podman.socket

# resolve and pin a base explicitly (optional; Dockerfile default is v256-cpu-b11011)
BASE=$(uv run scripts/resolve_base.py v256)

podman compose build \
  --build-arg LLAMA_SWAP_IMAGE=ghcr.io/mostlygeek/llama-swap:${BASE} \
  --build-arg PODMAN_VERSION=6.1.2

podman compose up -d
```

Or point compose at a published image instead of building: edit
`services.llama-swap.image` to
`ghcr.io/syakyr/llama-swap-podman:v256-podman1` and drop the `build:` block.

## Verified / unverified

Verified locally (2026-09-17):
- base image `v256-cpu-b11011` exists, multi-arch (amd64/arm64), entrypoint
  `/app/llama-swap -config /app/config.yaml -watch-config`
- podman `v6.1.2` ships `podman-remote-static-linux_{amd64,arm64}.tar.gz`
- `scripts/resolve_base.py` against live GHCR (v256, v255, and a bogus version)

Not yet exercised (first CI run will confirm):
- the `--help` / entrypoint smoke assertions in `build.yml`
- the watch workflow's dispatch loop end-to-end
