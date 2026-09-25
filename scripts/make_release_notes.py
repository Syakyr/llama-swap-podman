# /// script
# requires-python = ">=3.10"
# ///
"""Generate GitHub release notes for a llama-swap-podman tag.

Between our tags there are no changes of our own — only upstream bumps — so
these notes are a *pointer*, not a paraphrase. We state only facts we hold
(the exact pair, checksums, digest, size) and link to upstream's own release
page and CHANGELOG for the substance. We never summarise upstream changes we
have not read.

Usage (all required unless noted):
    uv run scripts/make_release_notes.py \
        --tag v259-podman6.1.2 --ls 259 --podman 6.2.0 \
        --ls-sha256 <hex> --podman-sha256 <hex> \
        --image ghcr.io/owner/repo:v259-podman6.1.2 \
        [--prev-tag v258-podman6.1.2-2] [--size-bytes N] [--smoke "11/11 passed"]

Reads GH_TOKEN from the environment (optional) to fetch a short upstream
release excerpt; without it, or on any fetch failure, it falls back to the
bare link rather than failing — a missing excerpt must never block a release.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request

UPSTREAM = {
    "llama-swap": "Mostlygeek/llama-swap",
    "podman": "podman-container-tools/podman",
}
TAG_RE = re.compile(r"^v(?P<ls>\d+)-podman(?P<pm>\d+\.\d+\.\d+)")


def get_json(url: str, token: str | None = None) -> dict:
    headers = {"User-Agent": "llama-swap-podman-release-notes"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=45) as r:
        return json.load(r)


def parse_tag(tag: str) -> tuple[str, str] | None:
    """v258-podman6.1.2-2 -> ("258", "6.1.2"); None if unparseable."""
    if not tag:
        return None
    m = TAG_RE.match(tag)
    return (m.group("ls"), m.group("pm")) if m else None


def ghcr_digest(image_ref: str) -> str:
    """Digest of the published manifest, so the notes cite the real artifact."""
    try:
        repo = image_ref.split("/", 1)[1].split(":")[0]
        tag = image_ref.rsplit(":", 1)[1]
        tok = get_json(f"https://ghcr.io/token?scope=repository:{repo}:pull")["token"]
        req = urllib.request.Request(
            f"https://ghcr.io/v2/{repo}/manifests/{tag}",
            headers={
                "Authorization": f"Bearer {tok}",
                "Accept": "application/vnd.oci.image.index.v1+json,"
                "application/vnd.docker.distribution.manifest.list.v2+json",
            },
        )
        with urllib.request.urlopen(req, timeout=45) as r:
            return r.headers.get("Docker-Content-Digest", "unknown")
    except Exception as exc:  # noqa: BLE001 - notes must not fail on a lookup
        return f"unavailable ({exc.__class__.__name__})"


def upstream_block(component: str, version: str, token: str | None) -> list[str]:
    repo = UPSTREAM[component]
    tag = version if version.startswith("v") else f"v{version}"
    url = f"https://github.com/{repo}/releases/tag/{tag}"
    lines = [f"**{component}** {version}", f"  Upstream release: {url}"]
    if component == "llama-swap":
        lines.append(
            f"  Changelog: https://github.com/{repo}/blob/main/CHANGELOG.md"
        )
    if token:
        try:
            data = get_json(f"https://api.github.com/repos/{repo}/releases/tags/{tag}", token)
            body = (data.get("body") or "").strip()
            excerpt = "\n".join(body.splitlines()[:12]).strip()
            if excerpt:
                lines.append("")
                lines.append("> " + excerpt.replace("\n", "\n> "))
        except Exception:  # noqa: BLE001 - excerpt is a nicety, not a requirement
            pass
    return lines


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--tag", required=True)
    p.add_argument("--ls", required=True, help="llama-swap version (bare, e.g. 259)")
    p.add_argument("--podman", required=True)
    p.add_argument("--ls-sha256", required=True)
    p.add_argument("--podman-sha256", required=True)
    p.add_argument("--image", required=True, help="full published image ref incl. tag")
    p.add_argument("--prev-tag", default="", help="previous release tag, if any")
    p.add_argument("--size-bytes", type=int, default=0)
    p.add_argument("--smoke", default="", help="smoke test summary line")
    a = p.parse_args()

    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    out: list[str] = []

    out.append("llama-swap aggregator image: the upstream release binary plus a "
               "static `podman-remote` CLI on `scratch`. Models are not run here — "
               "they are separate containers spawned on the host podman through the "
               "mounted socket.\n")

    prev = parse_tag(a.prev_tag)
    cur = (a.ls.lstrip("v"), a.podman)
    if not prev:
        out.append("**What moved:** first release recorded for this build scheme.\n")
    else:
        moved = []
        if prev[0] != cur[0]:
            moved.append(f"llama-swap {prev[0]} → {cur[0]}")
        if prev[1] != cur[1]:
            moved.append(f"podman-remote {prev[1]} → {cur[1]}")
        out.append("**What moved since %s:** %s\n"
                   % (a.prev_tag, ", ".join(moved) if moved else "nothing (rebuild of the same pair)"))

    for component, version in (("llama-swap", cur[0]), ("podman", cur[1])):
        out.extend(upstream_block(component, version, token))
        out.append("")

    out.append("## Provenance\n")
    out.append("| component | version | sha256 |")
    out.append("|---|---|---|")
    out.append(f"| llama-swap | {cur[0]} | `{a.ls_sha256}` |")
    out.append(f"| podman-remote | {cur[1]} | `{a.podman_sha256}` |\n")
    out.append("Both assets are verified against the checksums each upstream publishes "
               "during the build, and the hashes are recorded in the image labels.\n")
    out.append(f"- Image: `{a.image}`")
    out.append(f"- Digest: `{ghcr_digest(a.image)}`")
    if a.size_bytes:
        out.append(f"- Uncompressed size: `{a.size_bytes:,}` bytes")
    if a.smoke:
        out.append(f"- Smoke tests: {a.smoke}")

    out.append("\nOn-disk contract is unchanged: `/app/llama-swap`, "
               "`/app/config.yaml`, workdir `/app` — existing mounts carry over.\n")
    out.append("Docs: https://github.com/Syakyr/llama-swap-podman/tree/main/docs")

    print("\n".join(out))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except urllib.error.HTTPError as exc:
        sys.exit(f"release notes generation failed: HTTP {exc.code} for {exc.url}")
    except urllib.error.URLError as exc:
        sys.exit(f"release notes generation failed: {exc.reason}")
