# /// script
# requires-python = ">=3.10"
# ///
"""Resolve upstream release versions and their published sha256 checksums.

Replaces resolve_base.py: the image no longer builds on a GHCR base image, so
there are no `v<NNN>-cpu-b<build>` tags to page through. Instead we pin the
two release assets the Dockerfile downloads and verify them by checksum.

Usage:
    uv run scripts/resolve_release.py                      # newest of both
    uv run scripts/resolve_release.py v258                 # pinned llama-swap
    uv run scripts/resolve_release.py v258 6.1.2           # pinned pair
    uv run scripts/resolve_release.py --field ls_sha256    # just the hash

Options:
    --arch amd64|arm64   architecture the checksums are for (default amd64)
    --field NAME         print one scalar instead of the full JSON:
                       ls_version | ls_sha256 | podman_version | podman_sha256
    --shell              print eval-able exports for a build:
                           eval "$(uv run scripts/resolve_release.py --shell)"
                           podman build --build-arg LS_VERSION ... .

Both upstreams publish `<hash>  <filename>` sums (llama-swap:
llama-swap_<N>_checksums.txt, podman: `shasums`), which is what this reads —
not the GitHub API's own asset digests, so the value checked here is the value
upstream attests to.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.error
import urllib.request

LS_REPO = "Mostlygeek/llama-swap"
PODMAN_REPO = "podman-container-tools/podman"
SUM_RE = re.compile(r"^([0-9a-f]{64})\s+(\S+)\s*$")


def fetch(url: str) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": "llama-swap-podman-resolver"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.read()


def latest_release_tag(repo: str) -> str:
    """Newest *stable* release tag (GitHub's releases/latest excludes prereleases,
    so podman v7.0.0-rc1 style tags are never picked up)."""
    data = json.loads(fetch(f"https://api.github.com/repos/{repo}/releases/latest"))
    return data["tag_name"]


def parse_sums(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for line in text.splitlines():
        if m := SUM_RE.match(line):
            out[m.group(2)] = m.group(1)
    return out


def resolve(version: str | None, repo: str, sums_for, asset_for, label: str) -> tuple[str, dict[str, str]]:
    tag = (version or latest_release_tag(repo)).lstrip("v")
    asset = asset_for(tag)
    sums = parse_sums(fetch(sums_for(tag)).decode())
    missing = [a for a in asset.values() if a not in sums]
    if missing:
        sys.exit(f"{label} v{tag}: no checksum published for {', '.join(missing)}")
    return tag, {arch: sums[asset[arch]] for arch in asset}


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("llama_swap_version", nargs="?", help="e.g. v258 (blank = newest)")
    p.add_argument("podman_version", nargs="?", help="e.g. 6.1.2 (blank = newest stable)")
    p.add_argument("--arch", default="amd64", choices=["amd64", "arm64"])
    p.add_argument("--field", default=None, choices=[
        "ls_version", "ls_sha256", "podman_version", "podman_sha256",
    ])
    p.add_argument("--shell", action="store_true",
                 help="print eval-able LS_VERSION/LS_SHA256/PODMAN_VERSION/PODMAN_SHA256 exports")
    args = p.parse_args()

    ls_tag, ls_sums = resolve(
        args.llama_swap_version,
        LS_REPO,
        sums_for=lambda v: f"https://github.com/{LS_REPO}/releases/download/v{v}/llama-swap_{v}_checksums.txt",
        asset_for=lambda v: {a: f"llama-swap_{v}_linux_{a}.tar.gz" for a in ("amd64", "arm64")},
        label="llama-swap",
    )
    pm_tag, pm_sums = resolve(
        args.podman_version,
        PODMAN_REPO,
        sums_for=lambda v: f"https://github.com/{PODMAN_REPO}/releases/download/v{v}/shasums",
        asset_for=lambda v: {a: f"podman-remote-static-linux_{a}.tar.gz" for a in ("amd64", "arm64")},
        label="podman",
    )

    result = {
        "ls_version": ls_tag,
        "ls_sha256": ls_sums[args.arch],
        "ls_assets": {a: f"https://github.com/{LS_REPO}/releases/download/v{ls_tag}/llama-swap_{ls_tag}_linux_{a}.tar.gz" for a in ("amd64", "arm64")},
        "podman_version": pm_tag,
        "podman_sha256": pm_sums[args.arch],
        "podman_assets": {a: f"https://github.com/{PODMAN_REPO}/releases/download/v{pm_tag}/podman-remote-static-linux_{a}.tar.gz" for a in ("amd64", "arm64")},
        "pair_tag": f"v{ls_tag}-podman{pm_tag}",
    }

    if args.field:
        print(result[args.field])
    elif args.shell:
        print(f'export LS_VERSION="{ls_tag}"')
        print(f'export LS_SHA256="{ls_sums[args.arch]}"')
        print(f'export PODMAN_VERSION="{pm_tag}"')
        print(f'export PODMAN_SHA256="{pm_sums[args.arch]}"')
    else:
        print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except urllib.error.HTTPError as exc:
        # A 404 here means the version or the checksum asset does not exist
        # upstream; say so plainly instead of dumping a traceback into CI logs.
        sys.exit(f"lookup failed: HTTP {exc.code} for {exc.url}")
    except urllib.error.URLError as exc:
        sys.exit(f"lookup failed: {exc.reason}")
