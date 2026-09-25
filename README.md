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
![image size](https://img.shields.io/badge/image-50.6_MB_compressed-brightgreen)
![podman--remote](https://img.shields.io/badge/podman--remote-6.1.2-orange)

**Pull · Mount the socket · Done.**

</div>

---

## Why not the `cpu` image

llama-swap ships two kinds of image. Neither fits an aggregator:

| upstream image | compressed | what it carries |
|---|---|---|
| `llama-swap:<ver>-cpu` (what this repo used to wrap) | 331 MB | llama.cpp's `llama-server` base + llama-swap |
| `llama-swap:unified-*` | 586 MB – 5.5 GB | llama.cpp, ik-llama, whisper, stable-diffusion, audio, all built from source |
| **this image** | **50.6 MB** | llama-swap release binary, podman-remote, CA bundle, healthcheck |

In this deployment llama-swap never runs a model itself — `cmd:` in the
config is `podman run …`, so the model runtime lives in *another* image
(`ghcr.io/syakyr/halogen-timings-sidecar`, ROCm toolboxes, whatever).
The ~308 MB of llama.cpp and OS in the old base was dead weight, and
upstream now labels that image legacy anyway.

Verified against upstream v258 before switching:

- the release tarball is one **static ELF64 binary with no `PT_INTERP`**, so
  it runs on `scratch` (`./llama-swap -version` works bare)
- the Web UI is **compiled in** (goreleaser builds with `-tags embed_ui`),
  so no asset directory is needed
- llama-swap makes **no outbound HTTPS calls** itself; the CA bundle is here
  for users who point a model's `proxy:` at an `https://` host
- the only external binaries it execs are `nvidia-smi`/`rocm-smi`/`sysctl`
  for hardware detection (optional, degrades gracefully) and whatever your
  `cmd:` says — which is `podman run` here

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

Drop-in from the old image: the binary is still `/app/llama-swap`, the
config is still `/app/config.yaml`, the working dir is still `/app`.

Or use the included compose file (builds the same Dockerfile locally):

```bash
podman compose up -d --build
```

Verify the remote CLI inside the container sees your host daemon:

```bash
podman exec llama-swap podman info --format '{{.Host.RemoteSocket.Path}}'
```

## Healthcheck

The image ships a healthcheck, so `podman inspect` reports real liveness
instead of the `curl`-based one the base image had (which could not work
here anyway — scratch has no curl, and it probed a port this deployment
moves).

```bash
podman inspect -f '{{.State.Health.Status}}' llama-swap
```

It is a static Go probe (`healthcheck/`, unit tested) that checks:

1. **llama-swap answers** — `GET /health` (upstream's own endpoint), and
2. **the podman socket is reachable** — when `CONTAINER_HOST`/`PODMAN_HOST`
   declares a `unix://` socket. An aggregator that cannot reach the socket
   cannot spawn a model, so a live HTTP port alone is not health.

The probe URL is resolved in this order, so **a custom listen port needs no
configuration**:

1. `HEALTHCHECK_URL`, if set
2. the port llama-swap is actually listening on, read from its own command
   line (`/proc/1/cmdline` — it is PID 1 in this container), so
   `--listen 0.0.0.0:10301` just works
3. `http://127.0.0.1:8080/health`, the image default

| env | default | meaning |
|---|---|---|
| `HEALTHCHECK_URL` | *derived* | full endpoint to probe; overrides discovery |
| `HEALTHCHECK_TIMEOUT` | `3s` | per-probe timeout |
| `HEALTHCHECK_SKIP_SOCKET` | `false` | skip the socket check |

The socket check only runs when a `unix://` host is actually declared, so
running the image without a socket is not marked unhealthy.

## Images & tags

Prebuilt images: `ghcr.io/syakyr/llama-swap-podman`. A GitHub Actions
watcher checks **both** upstreams every 6 hours — the newest llama-swap
release *and* the newest stable podman release (`releases/latest` excludes
RCs) — and builds automatically whenever either moves. A podman bump alone
produces a new tag and a rebuild; an unchanged pair is skipped.

| Tag | Meaning |
|---|---|
| `latest` | newest llama-swap version with a built image |
| `v<NNN>` | latest build for that llama-swap version (moves on rebuild) |
| `v<NNN>-podman<X.Y.Z>` | immutable pairing of llama-swap `v<NNN>` with podman-remote `X.Y.Z` — pin this for reproducibility |
| `dev` | rolling build from `main`/`feat/**`, for testing before a release |
| `dev-<shortsha>` | the exact dev build for a commit |

```bash
# try the current branch before it is merged
podman pull ghcr.io/syakyr/llama-swap-podman:dev
```

Measured on the `dev` build (llama-swap v258 + podman 6.1.2):
**50.6 MB compressed / 96.4 MB uncompressed**, layers
`[0.1 CA bundle, 23.2 llama-swap, 24.8 podman-remote, 2.5 healthcheck]`.
The previous base-wrapped image was 380 MB compressed.

<details>
<summary>Don't want to wait up to 6 hours for a fresh upstream tag?</summary>

Run **watch-llama-swap** manually (`Actions → watch-llama-swap → Run
workflow`) and leave `llama_swap_version` blank to build the newest upstream
release now, or pin an exact one.

```bash
gh workflow run watch-llama-swap.yml -f llama_swap_version=v258
gh workflow run watch-llama-swap.yml -f llama_swap_version=v258 -f podman_version=6.1.2
```

The watcher triggers `build.yml` with an explicit `workflow_dispatch`
rather than relying on its own tag push: a tag pushed with `GITHUB_TOKEN`
does not start other workflows, so the tag is kept purely as provenance.

</details>

## Testing this branch against a live deployment

The `dev` tag is built from `main`/`feat/**` on every push, so a branch is
usable before it merges. Against the same mounts you already run:

```bash
# 1. pull and point your compose at the dev tag (image: only — same paths)
podman pull ghcr.io/syakyr/llama-swap-podman:dev

# 2. swap the container in place
podman stop llama-swap && podman rm llama-swap
podman run -d --name llama-swap \
  --network host \
  -e CONTAINER_HOST=unix:///podman.sock \
  -v /run/user/$(id -u)/podman/podman.sock:/podman.sock \
  -v ./config.yaml:/app/config.yaml \
  -v ./llama-swap-data:/data/llama-swap \
  ghcr.io/syakyr/llama-swap-podman:dev \
  -config /app/config.yaml -listen 0.0.0.0:8080 -watch-config

# 3. watch the healthcheck go green
watch -n5 "podman inspect -f '{{.State.Health.Status}}' llama-swap"

# 4. confirm it still drives the host daemon and can spawn a model
podman exec llama-swap podman info --format '{{.Host.RemoteSocket.Path}}'
curl -s localhost:8080/v1/models | head -c 200
```

Roll back by pointing the tag back at `:latest` (still the old base-wrapped
image until this merges).

**What to watch while testing:** the healthcheck goes `healthy` within its
start period and stays there while models swap; `podman` inside the container
still reaches your socket; nothing in your config needed to change. A
non-default listen port is discovered automatically — no `HEALTHCHECK_URL`
needed.

## Pinned by checksum, not by tag

Nothing is baked from a floating tag. Both release assets are pinned by
sha256, taken from the sums each upstream publishes (llama-swap's
`llama-swap_<N>_checksums.txt`, podman's `shasums` asset), verified during
the build, and recorded in the image labels:

```bash
uv run scripts/resolve_release.py            # newest of both upstreams
uv run scripts/resolve_release.py v258 6.1.2 --field ls_sha256
```

```
$ docker inspect -f '{{json .Config.Labels}}' <image> | python3 -m json.tool
{
  "io.llamaswap.binary.sha256": "8f0865e0…",
  "io.podman.remote.sha256":    "6785e4dc…",
  "org.opencontainers.image.version": "v258-podman6.1.2"
}
```

Upstream goreleaser does not sign (no cosign), so checksums are the
strongest integrity signal available; the build fails rather than pulling
an unverified asset.

## Build and test locally

```bash
# resolve the newest pair (bare version + checksums) and build
eval "$(uv run scripts/resolve_release.py --shell)"
#  LS_VERSION=258  LS_SHA256=8f0865e0…  PODMAN_VERSION=6.1.2  PODMAN_SHA256=6785e4dc…

podman build -t llama-swap-podman:local \
  --build-arg LS_VERSION="$LS_VERSION" --build-arg LS_SHA256="$LS_SHA256" \
  --build-arg PODMAN_VERSION="$PODMAN_VERSION" --build-arg PODMAN_SHA256="$PODMAN_SHA256" .

./scripts/smoke_test.sh llama-swap-podman:local "$LS" "$PM"
```

`scripts/smoke_test.sh` is what CI runs: version checks, `-validate`, CA
bundle presence, a live `/health`, and both halves of the healthcheck
including the negative cases (a probe that always returns 0 is worse than
no probe).

## Releasing a change

```bash
git tag v258-podman6.1.2 && git push origin v258-podman6.1.2
# → build.yml smoke-tests the image, then publishes
#   :v258-podman6.1.2 (immutable) and moves :v258 to it
```

`workflow_dispatch` on `build.yml` does the same interactively, with a
`mode` choice (`release` / `dev` / `test`) and optional checksum overrides.
Scheduled watcher runs always take the newest stable podman release. CI
also enforces a size budget (130 MB uncompressed) so nothing heavy creeps
back into the image.

## Layout

```
Dockerfile                 scratch-based build: fetch+verify, healthcheck build, runtime
healthcheck/               static Go liveness probe (unit tested)
scripts/resolve_release.py resolve upstream versions + published checksums
scripts/smoke_test.sh      the smoke suite CI runs against the built image
compose.yml                local run/deploy with the same pins
```

## Related

- [halogen-timings-sidecar](https://github.com/Syakyr/halogen-timings-sidecar) — a sibling wrapper; one of the model runtimes this aggregator spawns
- [llama-swap docs](https://github.com/Mostlygeek/llama-swap) — upstream configuration for container-based models
