# llama-swap CPU image with a static podman-remote CLI baked in, so the
# container can drive the host podman socket (CONTAINER_HOST=unix:///podman.sock).
#
# The base is pinned to an immutable v<ver>-cpu-b<build> tag (NOT the floating
# `cpu` tag) so every published image has a reproducible provenance record.
# The podman-remote download is pinned to a release version too; the asset
# name pattern podman-remote-static-linux_{amd64,arm64}.tar.gz was verified
# against the v6.1.2 release. Repo org was renamed containers ->
# podman-container-tools; the old path 301-redirects (ADD follows it), but
# we point at the current name so the URL stays honest.
ARG LLAMA_SWAP_IMAGE=ghcr.io/mostlygeek/llama-swap:v257-cpu-b11151
FROM ${LLAMA_SWAP_IMAGE}

ARG PODMAN_VERSION=6.1.2
ARG TARGETARCH=amd64

ADD https://github.com/podman-container-tools/podman/releases/download/v${PODMAN_VERSION}/podman-remote-static-linux_${TARGETARCH}.tar.gz /tmp/podman-remote.tar.gz

RUN tar -xzf /tmp/podman-remote.tar.gz -C /tmp && \
    install -m 0755 /tmp/bin/podman-remote-static-linux_${TARGETARCH} /usr/local/bin/podman && \
    rm -rf /tmp/podman-remote.tar.gz /tmp/bin

# Base ENTRYPOINT (/app/llama-swap -config /app/config.yaml -watch-config)
# and WORKDIR /app are intentionally preserved; compose appends its own flags.
