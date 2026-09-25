# Releasing

## Cut a release

```bash
git tag v258-podman6.1.2 && git push origin v258-podman6.1.2
```

`build.yml` resolves the pair, builds, smoke-tests, then publishes:

| tag | |
|---|---|
| `v258-podman6.1.2` | immutable pairing record |
| `v258` | latest build of that llama-swap version |
| `latest` | only when `v258` is the newest upstream release |

A tag whose pairing already exists is overwritten on GHCR. To keep a distinct
immutable record, cut a different podman patch or use a dispatch with
explicit checksums.

**Release notes go on the GitHub release, not in the README.** The README is
a static description of what the thing is; "what changed" belongs on
`Releases → Draft a new release` and never in the repo's landing page.

## The watcher

`watch-llama-swap.yml` runs every 6 hours:

1. resolves the newest llama-swap release and the newest **stable** podman
   release (GitHub's `releases/latest` excludes prereleases, so
   `v7.0.0-rc1`-style tags are never picked up)
2. checks whether `v<NNN>-podman<X.Y.Z>` already exists in GHCR
3. if not, dispatches `build.yml` in `release` mode with both versions and
   both checksums
4. on a newest-only resolution, commits a pin bump to `Dockerfile` and
   `compose.yml` so bare local builds never rot

A manual watcher run pinned to an older version deliberately skips step 4 —
defaults track newest-only.

A tag pushed with `GITHUB_TOKEN` does not start other workflows, so the
watcher dispatches `build.yml` explicitly rather than relying on its own tag
push; the tag is provenance only.

```bash
gh workflow run watch-llama-swap.yml -f llama_swap_version=v258
gh workflow run watch-llama-swap.yml -f llama_swap_version=v258 -f podman_version=6.1.2
gh workflow run watch-llama-swap.yml -f force=true      # rebuild an existing pairing
```

## Dispatch modes

`build.yml` takes a `mode` choice:

| mode | publishes | use |
|---|---|---|
| `release` | `v<NNN>-podman<X.Y.Z>`, `v<NNN>`, `latest` | cutting a release from a branch ref; requires both versions as inputs |
| `dev` | `dev`, `dev-<shortsha>` | pre-release testing |
| `test` | nothing | build + smoke test only |

`auto` maps tag pushes to `release`, branch pushes to `dev`, PRs to `test`.

## Dev images

Every push to `main` or `feat/**` publishes `:dev` and `:dev-<shortsha>`
after the full smoke suite passes. Use `:dev` to try a branch before merging;
pin `:dev-<shortsha>` if you need the exact build you tested to stay stable
while the branch moves.

## Rollback

Point the tag back:

```bash
podman pull ghcr.io/syakyr/llama-swap-podman:v257-podman6.1.2   # or whatever is known-good
# recreate the container on that tag; mounts are unchanged
```

Because the on-disk contract is fixed (`/app/llama-swap`,
`/app/config.yaml`), a rollback is a tag change and nothing else.
