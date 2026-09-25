# Design

## The aggregator premise

llama-swap here never runs a model. Its `cmd:` is `podman run …`, so every
model runtime is a **separate container** spawned on the host podman through
the mounted socket. The llama-swap process only routes, swaps, and proxies.

That single fact decides the image: anything in it that exists to run models
locally is dead weight.

## Why not the upstream images

| image | compressed | carries |
|---|---|---|
| `llama-swap:<ver>-cpu` | 331 MB | llama.cpp's `llama-server` base + llama-swap |
| `llama-swap:unified-cuda13` | 5.5 GB | llama.cpp, ik-llama, whisper, stable-diffusion, audio, built from source |
| `llama-swap:unified-vulkan` | 586 MB | as above, Vulkan |
| **this image** | **~50 MB** | llama-swap binary, podman-remote, CA bundle, healthcheck |

Of the old `cpu` base's 331 MB, roughly 308 MB is llama.cpp plus an OS this
image never executes — layers that are pulled, stored, and scanned but never
run. Upstream labels that image *legacy* and points at `unified`, which is
further from an aggregator than the `cpu` image is.

Building from the release binary on `scratch` inverts that: the image is
exactly the two binaries the role requires.

## Properties of the upstream release binary

These are what make a `scratch` runtime viable at all:

- **Static ELF64 with no `PT_INTERP`** — no dynamic loader, no libc, so it
  executes on `scratch` with nothing else present.
- **Web UI compiled in** — goreleaser builds with `-tags embed_ui`, so there
  is no asset directory to ship.
- **No outbound HTTPS in core** — upstream probes are plain HTTP to local
  model servers. The CA bundle in this image exists only for users who point
  a model's `proxy:` at an `https://` host.
- **Small exec surface** — the only external binaries llama-swap invokes are
  `nvidia-smi` / `rocm-smi` / `sysctl` for hardware detection (optional,
  degrades gracefully) and whatever `cmd:` specifies.

## Runtime contract

Fixed paths, so mounts and compose files never change:

| path | what |
|---|---|
| `/app/llama-swap` | the binary |
| `/app/config.yaml` | config (mounted) |
| `/app/healthcheck` | the liveness probe |
| `/usr/local/bin/podman` | static podman-remote |
| `/app` | working directory |

`ENTRYPOINT` is the bare binary and `CMD` holds the default flags, so a
compose `command:` **replaces** the defaults rather than appending duplicate
flags to a flag-carrying entrypoint.

`HOME` is `/tmp`: `scratch` has no `/etc/passwd`, and podman-remote resolves
its client config relative to `HOME`.

## Security posture

The image runs as root with no `USER` directive, which keeps the mounted
socket and config simple. Mount only what the aggregator needs — the socket
grants control of the host daemon, so treat it as the sensitive mount it is.
Add a `USER` directive if your deployment can tolerate it; nothing in the
image requires root at runtime.
