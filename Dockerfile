# syntax=docker/dockerfile:1.7
#
# llama-swap aggregator image: the upstream *release* binary plus a static
# podman-remote CLI, on scratch.
#
# This replaces the previous approach of layering podman-remote on top of
# ghcr.io/mostlygeek/llama-swap:<ver>-cpu (llama.cpp's server image with
# llama-swap copied in). That base is 331 MB compressed, of which ~308 MB is
# llama.cpp and an OS this image never uses: llama-swap runs here purely as an
# aggregator/router, and every model runtime is a *separate* container spawned
# on the host podman through the mounted socket. Nothing in the aggregator
# role needs llama.cpp.
#
# Verified against upstream v258 before switching:
#   - the release tarball is one static ELF64 binary with no PT_INTERP, so it
#     runs on scratch (./llama-swap -version works bare)
#   - the Web UI is compiled in (goreleaser builds with -tags embed_ui), so no
#     asset directory is needed
#   - llama-swap itself makes no outbound HTTPS calls; the CA bundle below is
#     belt-and-braces for users who point a model's proxy: at an https:// host
#   - the only external binaries it execs are nvidia-smi/rocm-smi/sysctl for
#     hardware detection (optional, degrades gracefully) and whatever `cmd:`
#     says — which is `podman run` here
#
# Both downloads are sha256-verified against the checksums each upstream
# publishes (llama-swap_<N>_checksums.txt and podman's `shasums` asset).
# Resolve them with:
#
#   uv run scripts/resolve_release.py            # newest of both upstreams
#   uv run scripts/resolve_release.py v258 6.1.2 # explicit pair
#
# The image keeps the legacy base's on-disk contract so existing mounts and
# compose files work unchanged: binary at /app/llama-swap, config expected at
# /app/config.yaml, working dir /app.

ARG LS_VERSION=258
ARG LS_SHA256=8f0865e08940be7acf2adac97f736ef0f7bf56d969b1f77b10ea395a29fab9f9
ARG PODMAN_VERSION=6.1.2
ARG PODMAN_SHA256=6785e4dc11dad67000308749fed0f981698792309830a6b870bd5a97b3527182

# ---------------------------------------------------------------------------
# fetch: download both upstream release assets and verify them before anything
# else touches them. A mismatch fails the build; there is no "skip" default.
# ---------------------------------------------------------------------------
FROM alpine:3.22 AS fetch

ARG LS_VERSION
ARG LS_SHA256
ARG PODMAN_VERSION
ARG PODMAN_SHA256
ARG TARGETARCH=amd64

RUN apk add --no-cache curl ca-certificates

# LS_VERSION is a bare number (258); the fetch tolerates a stray v prefix.
RUN set -eux; \
    if [ -z "${LS_SHA256}" ] || [ -z "${PODMAN_SHA256}" ]; then \
        echo "LS_SHA256 and PODMAN_SHA256 are required." >&2; \
        echo "Get them with: uv run scripts/resolve_release.py ${LS_VERSION} ${PODMAN_VERSION}" >&2; \
        exit 1; \
    fi; \
    LS_VER="${LS_VERSION#v}"; \
    mkdir -p /fetch; \
    cd /fetch; \
    ls_asset="llama-swap_${LS_VER}_linux_${TARGETARCH}.tar.gz"; \
    pm_asset="podman-remote-static-linux_${TARGETARCH}.tar.gz"; \
    curl -fsSL --retry 3 --retry-all-errors -o "$ls_asset" \
        "https://github.com/Mostlygeek/llama-swap/releases/download/v${LS_VER}/${ls_asset}"; \
    echo "${LS_SHA256}  ${ls_asset}" | sha256sum -c -; \
    tar -xzf "$ls_asset" llama-swap; \
    curl -fsSL --retry 3 --retry-all-errors -o "$pm_asset" \
        "https://github.com/podman-container-tools/podman/releases/download/v${PODMAN_VERSION}/${pm_asset}"; \
    echo "${PODMAN_SHA256}  ${pm_asset}" | sha256sum -c -; \
    tar -xzf "$pm_asset" "bin/podman-remote-static-linux_${TARGETARCH}"; \
    test -x /fetch/llama-swap; \
    test -x "/fetch/bin/podman-remote-static-linux_${TARGETARCH}"

# ---------------------------------------------------------------------------
# healthcheck: static probe replacing the upstream image's
# `CMD-SHELL curl -f http://localhost:8080/`. scratch has no shell and no
# curl, so the probe is compiled in. Tests run here so a plain `podman build`
# verifies the probe too, not just CI.
# ---------------------------------------------------------------------------
FROM golang:1.27-alpine AS healthcheck

WORKDIR /src
COPY healthcheck/go.mod healthcheck/main.go healthcheck/main_test.go ./

RUN set -eux; \
    go vet ./...; \
    go test ./...; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /healthcheck .; \
    test -x /healthcheck

# ---------------------------------------------------------------------------
# certs: CA bundle only, so an https:// upstream proxy verifies. debian slim
# ships ca-certificates; assert it rather than assume it survived a base bump.
# ---------------------------------------------------------------------------
FROM debian:trixie-slim AS certs
RUN test -s /etc/ssl/certs/ca-certificates.crt

# ---------------------------------------------------------------------------
# runtime
# ---------------------------------------------------------------------------
FROM scratch

ARG LS_VERSION
ARG LS_SHA256
ARG PODMAN_VERSION
ARG PODMAN_SHA256
ARG TARGETARCH=amd64

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chmod=0755 --from=fetch /fetch/llama-swap /app/llama-swap
COPY --chmod=0755 --from=fetch "/fetch/bin/podman-remote-static-linux_${TARGETARCH}" /usr/local/bin/podman
COPY --chmod=0755 --from=healthcheck /healthcheck /app/healthcheck

# scratch has no /etc/passwd; HOME is set to a writable path because
# podman-remote resolves config (~/.config/containers) relative to it.
ENV HOME=/tmp

WORKDIR /app

# Same entrypoint shape as the legacy base, split so that passing a `command:`
# in compose *replaces* the defaults instead of appending duplicate flags
# (the old ENTRYPOINT carried -config/-watch-config, so a compose command
# produced them twice).
ENTRYPOINT ["/app/llama-swap"]
CMD ["-config", "/app/config.yaml", "-watch-config"]

# Parity with the upstream image's healthcheck, without curl: GET /health on
# the default port (override with HEALTHCHECK_URL) and, when CONTAINER_HOST
# declares a unix:// socket, confirm that socket is reachable — an aggregator
# that cannot reach the podman socket is not a working aggregator.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/app/healthcheck"]

LABEL \
    org.opencontainers.image.title="llama-swap-podman" \
    org.opencontainers.image.description="llama-swap aggregator (upstream release binary) with podman-remote, on scratch" \
    org.opencontainers.image.source="https://github.com/Syakyr/llama-swap-podman" \
    org.opencontainers.image.version="v${LS_VERSION}-podman${PODMAN_VERSION}" \
    io.llamaswap.binary.sha256="${LS_SHA256}" \
    io.podman.remote.sha256="${PODMAN_SHA256}"
