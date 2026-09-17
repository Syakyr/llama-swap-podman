<div align="center">

# 🦙 llama-swap + Podman

### llama-swap that spawns its models on your **host** podman.

A wrapped [llama-swap](https://github.com/Mostlygeek/llama-swap) CPU image
with a static **podman-remote** CLI baked in, so the container talks to your
user podman socket (`CONTAINER_HOST=unix:///podman.sock`) instead of needing
a runtime inside itself.

[![build](https://github.com/syakyr/llama-swap-podman/actions/workflows/build.yml/badge.svg)](https://github.com/syakyr/llama-swap-podman/actions/workflows/build.yml)
[![watch-llama-swap](https://github.com/syakyr/llama-swap-podman/actions/workflows/watch-llama-swap.yml/badge.svg)](https://github.com/syakyr/llama-swap-podman/actions/workflows/watch-llama-swap.yml)
![latest build](https://img.shields.io/github/v/tag/Syakyr/llama-swap-podman?label=latest%20build&color=brightgreen)
![podman-remote](https://img.shields.io/badge/podman--remote-6.1.2-orange)
![base](https://img.shields.io/badge/base-llama--swap%3Av256--cpu-blue)

**Pull · Mount the socket · Done.**

</div>

---

## The problem, in one mount

llama-swap in a container cannot spawn model containers unless the image
carries a container client *and* can reach a daemon. This image ships
`podman-remote` (static, no deps); you supply the daemon by mounting your
user socket:

```
llama-swap container ──/podman.sock──▶ ~/.local/share/…/podman/podman.sock (host)
        │                                     │
        └── CONTAINER_HOST=unix:///podman.sock ── model containers run on the host
```

Every byte of llama-swap itself is upstream — the only change is the image name.

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

Or use the included compose file (builds the same Dockerfile locally):

```bash
podman compose up -d --build
```

Verify the remote CLI inside the container sees your host daemon:

```bash
podman exec llama-swap podman info --format '{{.Host.RemoteSocket.Path}}'
```

## Images & tags

Prebuilt images: `ghcr.io/syakyr/llama-swap-podman`. A GitHub Actions
watcher checks **both** upstreams every 6 hours — the newest llama-swap
release *and* the newest stable podman release (`releases/latest` excludes
RCs) — and builds automatically whenever either moves. A podman bump alone
produces a new tag (`v256-podman6.2.0`) and a rebuild; an unchanged pair
is skipped. No manual build needed, ever.

<details>
<summary>Don't want to wait up to 6 hours for a fresh upstream tag?</summary>

Run **watch-llama-swap** manually (`Actions → watch-llama-swap → Run
workflow`) and leave `llama_swap_version` blank to build the newest upstream
release now, or pin an exact one.

```bash
# same thing from the CLI
gh workflow run watch-llama-swap.yml -f llama_swap_version=v256
# optionally pin the podman side too:
gh workflow run watch-llama-swap.yml -f llama_swap_version=v256 -f podman_version=6.1.2
```

Note that the watcher triggers `build.yml` with an explicit
`workflow_dispatch` rather than relying on its own tag push: a tag pushed
with `GITHUB_TOKEN` does not start other workflows (only
`workflow_dispatch`/`repository_dispatch` cross that boundary), so the tag
is kept purely as provenance.

</details>

| Tag | Meaning |
|---|---|
| `latest` | newest llama-swap version with a built image |
| `v<NNN>` | latest build for that llama-swap version (moves on rebuild) |
| `v<NNN>-podman<X.Y.Z>` | immutable pairing of llama-swap `v<NNN>` with podman-remote `X.Y.Z` — pin this for reproducibility |

The tag encodes both upstream versions: `v256-podman6.1.2` is exactly
llama-swap v256 wrapped with podman-remote 6.1.2. Bumping the podman side
is just a different tag: `v256-podman7.0.0`.

<details>
<summary>List available tags without pulling</summary>

```bash
curl -s "https://ghcr.io/token?scope=repository:syakyr/llama-swap-podman:pull" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' \
  | xargs -I{} curl -s -H "Authorization: Bearer {}" \
      https://ghcr.io/v2/syakyr/llama-swap-podman/tags/list \
  | python3 -m json.tool
```

</details>

## Pinned bases, resolved not assumed

The floating `cpu` tag is never baked in blindly. Each build resolves the
newest **immutable** `v<NNN>-cpu-b<build>` tag on GHCR and records it in
the run summary:

```bash
uv run scripts/resolve_base.py v256
# → v256-cpu-b11011
```

`build.yml` accepts a `base_tag` input to override the resolution with an
exact base if you need to rebuild against a specific upstream build.

## Build locally

```bash
BASE=$(uv run scripts/resolve_base.py v256)
podman build -t llama-swap-podman:local \
  --build-arg LLAMA_SWAP_IMAGE=ghcr.io/mostlygeek/llama-swap:${BASE} \
  --build-arg PODMAN_VERSION=6.1.2 .
```

Then run it exactly as in the Quick start, swapping the image name.

## Releasing a change

```bash
git tag v256-podman6.1.2 && git push origin v256-podman6.1.2
# → build.yml smoke-tests the image, then publishes
#   :v256-podman6.1.2 (immutable) and moves :v256 to it
```

`workflow_dispatch` on `build.yml` does the same interactively (with
explicit `llama_swap_version` / `podman_version` inputs). Scheduled
watcher runs always take the newest stable podman release; pin it with the
`podman_version` input on a manual watcher run if you need to hold it
back. Re-publishing an identical `v<NNN>-podman<X.Y.Z>` pairing
overwrites that GHCR tag, so cut a new podman patch (or use `base_tag`
overrides in a dispatch) when you need a distinct immutable record.

## Related

- [halogen-timings-sidecar](https://github.com/Syakyr/halogen-timings-sidecar) — the sibling wrapper this repo's CI pattern is copied from
- [llama-swap docs: containers](https://github.com/Mostlygeek/llama-swap) — upstream configuration for container-based models
