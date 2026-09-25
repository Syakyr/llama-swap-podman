# Building

## Nothing floats

Both release assets are pinned by **sha256**, taken from the sums each
upstream publishes and verified during the build:

- llama-swap: `llama-swap_<N>_checksums.txt`
- podman: the `shasums` release asset

The build refuses an unverified asset. Upstream goreleaser does not sign
(no cosign), so checksums are the strongest integrity signal available.
The verified hashes are recorded in the image labels:

```bash
docker inspect -f '{{json .Config.Labels}}' <image> | python3 -m json.tool
# io.llamaswap.binary.sha256 / io.podman.remote.sha256
# org.opencontainers.image.version = v<NNN>-podman<X.Y.Z>
```

## Resolving versions and checksums

```bash
uv run scripts/resolve_release.py                  # newest of both upstreams
uv run scripts/resolve_release.py v258 6.1.2       # explicit pair
uv run scripts/resolve_release.py --field ls_sha256
eval "$(uv run scripts/resolve_release.py --shell)" # LS_VERSION / LS_SHA256 / PODMAN_VERSION / PODMAN_SHA256
uv run scripts/resolve_release.py --arch arm64
```

`LS_VERSION` is a bare number (`258`); tags keep the `v` prefix.

## Build locally

```bash
eval "$(uv run scripts/resolve_release.py --shell)"

podman build -t llama-swap-podman:local \
  --build-arg LS_VERSION="$LS_VERSION" --build-arg LS_SHA256="$LS_SHA256" \
  --build-arg PODMAN_VERSION="$PODMAN_VERSION" --build-arg PODMAN_SHA256="$PODMAN_SHA256" .
```

`compose.yml` carries the same pins as defaults, so `podman compose build`
uses whatever the watcher last synced.

The build has four stages:

| stage | base | does |
|---|---|---|
| `fetch` | alpine | downloads both assets, `sha256sum -c` verifies, extracts |
| `healthcheck` | golang | `go vet`, `go test`, static build of the probe |
| `certs` | debian slim | installs the CA bundle |
| runtime | `scratch` | copies the four artifacts in |

Tests run inside the build, so a plain `podman build` verifies the probe too
rather than trusting CI alone.

## Smoke test

```bash
./scripts/smoke_test.sh llama-swap-podman:local 258 6.1.2
```

This is what CI runs. It checks, in order:

1. llama-swap reports the expected version
2. the podman client runs
3. config validation works inside the image
4. the CA bundle is present and is real PEM
5. llama-swap serves `/health`
6. the healthcheck passes against a live server
7. the healthcheck **fails** against a dead endpoint
8. the socket probe passes with a live socket mounted
9. the socket probe **fails** when the declared socket is missing
10. the healthcheck discovers a non-default listen port on its own

The negative cases matter: a probe that always returns 0 is worse than no
probe.

## Size budget

CI fails the build above **130 MB uncompressed**. The runtime is
llama-swap (~43 MB) + podman-remote (~47 MB) + CA bundle + probe (~6 MB),
so a jump past the budget means something heavy crept in.

```bash
docker image inspect -f '{{.Size}}' <image>
```

## Multi-arch

Both upstreams publish `amd64` and `arm64` assets and the Dockerfile is
arch-aware via `TARGETARCH`, but CI builds `amd64` only today. Adding
`platforms: linux/amd64,linux/arm64` to the push steps is the whole change.
