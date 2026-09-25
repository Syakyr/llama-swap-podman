#!/usr/bin/env bash
# Smoke test a llama-swap-podman image end to end.
#
# Usage:
#   scripts/smoke_test.sh <image> [llama_swap_version] [podman_version]
#
# Runs against $DOCKER (default: docker). Covers:
#   1. llama-swap binary reports the expected version
#   2. podman-remote client runs
#   3. config validation works inside the image (-validate)
#   4. CA bundle is present (https:// upstreams need it; scratch has none by default)
#   5. llama-swap actually serves and /health answers 200
#   6. the built-in healthcheck passes against a live server
#   7. the healthcheck FAILS against a dead endpoint (a probe that always
#      returns 0 is worse than no probe)
#   8. the podman socket probe passes with a live socket mounted
#   9. the podman socket probe fails when the declared socket is absent
set -euo pipefail

IMAGE="${1:?usage: smoke_test.sh <image> [llama_swap_version] [podman_version]}"
LS_VERSION="${2:-}"
PODMAN_VERSION="${3:-}"
DOCKER="${DOCKER:-docker}"

WORK="$(mktemp -d)"
SOCK_PID=""
CID=""
cleanup() {
    [ -n "$CID" ] && "$DOCKER" rm -f "$CID" >/dev/null 2>&1 || true
    [ -n "$SOCK_PID" ] && kill "$SOCK_PID" >/dev/null 2>&1 || true
    rm -rf "$WORK"
}
trap cleanup EXIT

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1" >&2; exit 1; }

echo "== $IMAGE =="

# 1. llama-swap version -----------------------------------------------------
out="$("$DOCKER" run --rm --entrypoint /app/llama-swap "$IMAGE" -version 2>&1)"
echo "  llama-swap: $out"
if [ -n "$LS_VERSION" ]; then
    echo "$out" | grep -q "v${LS_VERSION#v}" || fail "expected llama-swap v${LS_VERSION} in: $out"
fi
pass "llama-swap binary responds with the expected version"

# 2. podman client ---------------------------------------------------------
out="$("$DOCKER" run --rm --entrypoint podman "$IMAGE" --version 2>&1)"
echo "  podman: $out"
if [ -n "$PODMAN_VERSION" ]; then
    echo "$out" | grep -q "${PODMAN_VERSION}" || fail "expected podman ${PODMAN_VERSION} in: $out"
fi
pass "podman-remote client runs"

# 3. config validation -----------------------------------------------------
# NB: overriding --entrypoint also drops the image's default CMD, so -config
# must be passed explicitly here or llama-swap has no config to validate.
printf 'models: {}\n' >"$WORK/config.yaml"
out="$("$DOCKER" run --rm --entrypoint /app/llama-swap \
    -v "$WORK/config.yaml:/app/config.yaml:ro" "$IMAGE" -config /app/config.yaml -validate 2>&1)"
echo "$out" | grep -qi "valid" || fail "-validate did not report valid: $out"
pass "config validates inside the image"

# 4. CA bundle -------------------------------------------------------------
cid_certs="$("$DOCKER" create "$IMAGE")"
"$DOCKER" cp "$cid_certs:/etc/ssl/certs/ca-certificates.crt" "$WORK/ca.crt" >/dev/null
"$DOCKER" rm -f "$cid_certs" >/dev/null
[ -s "$WORK/ca.crt" ] || fail "no CA bundle at /etc/ssl/certs/ca-certificates.crt"
grep -q "BEGIN CERTIFICATE" "$WORK/ca.crt" || fail "CA bundle is not a PEM bundle"
pass "CA bundle present ($(grep -c 'BEGIN CERTIFICATE' "$WORK/ca.crt") certs)"

# 5. llama-swap serves -----------------------------------------------------
CID="$("$DOCKER" run -d --rm \
    -v "$WORK/config.yaml:/app/config.yaml:ro" \
    -p 127.0.0.1:18080:8080 \
    "$IMAGE" -config /app/config.yaml -listen 0.0.0.0:8080)"
for _ in $(seq 1 30); do
    code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/health || true)"
    [ "$code" = "200" ] && break
    sleep 1
done
[ "$code" = "200" ] || fail "llama-swap did not answer /health with 200 (last: $code)"
pass "llama-swap serves /health from inside the container"

# 6. healthcheck passes ----------------------------------------------------
"$DOCKER" exec "$CID" /app/healthcheck || fail "healthcheck failed against a live server"
pass "built-in healthcheck passes (HTTP up, no socket declared -> none checked)"

# 7. healthcheck fails on a dead endpoint ----------------------------------
if "$DOCKER" exec -e HEALTHCHECK_URL="http://127.0.0.1:9/health" \
        "$CID" /app/healthcheck >/dev/null 2>&1; then
    fail "healthcheck returned 0 against a dead endpoint"
fi
pass "built-in healthcheck fails against a dead endpoint"

# 8. socket probe passes ---------------------------------------------------
# A live unix socket stands in for the host podman socket; the probe container
# shares the running llama-swap's network namespace so the HTTP half resolves.
python3 - "$WORK/proxy.sock" <<'PY' &
import os, socket, sys, time
path = sys.argv[1]
if os.path.exists(path):
    os.remove(path)
s = socket.socket(socket.AF_UNIX)
s.bind(path)
s.listen(8)
time.sleep(120)
PY
SOCK_PID=$!
for _ in $(seq 1 20); do [ -S "$WORK/proxy.sock" ] && break; sleep 0.25; done
[ -S "$WORK/proxy.sock" ] || fail "test fixture socket never appeared"

"$DOCKER" run --rm --net "container:$CID" \
    -v "$WORK/proxy.sock:/podman.sock:ro" \
    -e CONTAINER_HOST=unix:///podman.sock \
    --entrypoint /app/healthcheck "$IMAGE" \
    || fail "socket probe failed with a live socket mounted"
pass "podman socket probe passes with the socket mounted"

# 9. socket probe fails when absent ----------------------------------------
if "$DOCKER" run --rm --net "container:$CID" \
        -e CONTAINER_HOST=unix:///podman.sock \
        --entrypoint /app/healthcheck "$IMAGE" >/dev/null 2>&1; then
    fail "socket probe returned 0 with CONTAINER_HOST set but no socket present"
fi
pass "podman socket probe fails when the declared socket is missing"

echo "all smoke tests passed"
