# /// script
# requires-python = ">=3.10"
# ///
"""Resolve the newest immutable v<ver>-cpu-b<build> tag on GHCR for a llama-swap version.

Usage: uv run scripts/resolve_base.py v256
Prints e.g. "v256-cpu-b11011" (highest build number) to stdout.

Uses an anonymous GHCR pull token and follows the Link header to page through
the full tag list (a single page is capped at 1000 and is NOT sorted by
version, so index 0 / last page tricks are unreliable here).
"""
import json
import re
import sys
import urllib.request

REPO = "mostlygeek/llama-swap"
TAG_RE = re.compile(r"^v(?P<ver>\d+)-cpu-b(?P<build>\d+)$")


def pull_token() -> str:
    url = f"https://ghcr.io/token?scope=repository:{REPO}:pull"
    with urllib.request.urlopen(url) as resp:
        return json.load(resp)["token"]


def list_tags(tok: str) -> list[str]:
    tags: list[str] = []
    url = f"https://ghcr.io/v2/{REPO}/tags/list?n=1000"
    while url:
        req = urllib.request.Request(url, headers={"Authorization": f"Bearer {tok}"})
        with urllib.request.urlopen(req) as resp:
            tags += json.load(resp).get("tags", [])
            link = resp.headers.get("Link", "")
        m = re.search(r'<([^>]+)>;\s*rel="next"', link)
        if not m:
            break
        nxt = m.group(1)
        url = nxt if nxt.startswith("http") else f"https://ghcr.io{nxt}"
    return tags


def main() -> int:
    if len(sys.argv) != 2 or not re.fullmatch(r"v?\d+", sys.argv[1]):
        sys.exit("usage: resolve_base.py <version>   (e.g. v256 or 256)")
    want = sys.argv[1].lstrip("v")

    builds = [
        int(m.group("build"))
        for t in list_tags(pull_token())
        if (m := TAG_RE.match(t)) and m.group("ver") == want
    ]
    if not builds:
        sys.exit(f"no v{want}-cpu-b* tags found on ghcr.io/{REPO}")
    print(f"v{want}-cpu-b{max(builds)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
