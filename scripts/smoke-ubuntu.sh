#!/usr/bin/env bash
# smoke-ubuntu.sh — post-install smoke test for MTProto Proxy Orchestrator
# Run on a clean Ubuntu 22.04+ VM after a successful tgproxy-cli install.
# Usage: sudo bash scripts/smoke-ubuntu.sh
set -uo pipefail

PASS=0
FAIL=0

green='\033[0;32m'
red='\033[0;31m'
reset='\033[0m'

ok() {
    echo -e "  ${green}PASS${reset}  $1"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "  ${red}FAIL${reset}  $1"
    FAIL=$((FAIL + 1))
}

check() {
    local description="$1"
    shift
    if "$@" >/dev/null 2>&1; then
        ok "$description"
    else
        fail "$description"
    fi
}

echo "=== MTProto Proxy Orchestrator — Ubuntu Smoke Test ==="
echo

# ── 1. OS check ──────────────────────────────────────────────────────────────
echo "[ OS ]"
if grep -qi 'ubuntu' /etc/os-release 2>/dev/null; then
    ver=$(grep VERSION_ID /etc/os-release | cut -d'"' -f2)
    major=${ver%%.*}
    if [ "${major:-0}" -ge 22 ]; then
        ok "Ubuntu ${ver} (>= 22.04)"
    else
        fail "Ubuntu ${ver} is older than 22.04"
    fi
else
    fail "Not running on Ubuntu — os-release does not mention Ubuntu"
fi
echo

# ── 2. CLI binary ─────────────────────────────────────────────────────────────
echo "[ CLI binary ]"
check "tgproxy-cli exists at /usr/local/bin/tgproxy-cli" test -x /usr/local/bin/tgproxy-cli
echo

# ── 3. tgproxy-cli status ─────────────────────────────────────────────────────
echo "[ tgproxy-cli status ]"
if /usr/local/bin/tgproxy-cli status >/dev/null 2>&1; then
    ok "tgproxy-cli status exits 0"
else
    fail "tgproxy-cli status exited non-zero"
fi
echo

# ── 4. systemd services ───────────────────────────────────────────────────────
echo "[ systemd services ]"
for svc in teleproxy.service tgproxy-panel.service; do
    if systemctl is-active --quiet "$svc" 2>/dev/null; then
        ok "$svc is active"
    else
        fail "$svc is NOT active ($(systemctl is-active "$svc" 2>/dev/null || echo 'unknown'))"
    fi
done
echo

# ── 5. nginx ──────────────────────────────────────────────────────────────────
echo "[ nginx ]"
if systemctl is-active --quiet nginx 2>/dev/null; then
    ok "nginx is active"
else
    fail "nginx is NOT active"
fi
echo

# ── 6. Panel health endpoint ──────────────────────────────────────────────────
echo "[ Panel health endpoint ]"
http_code=$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' http://127.0.0.1:8443/health 2>/dev/null || true)
if [ "$http_code" = "204" ]; then
    ok "Panel health endpoint responded on loopback backend (HTTP $http_code)"
elif [ -n "$http_code" ] && [ "$http_code" != "000" ]; then
    fail "Panel health endpoint returned unexpected status (HTTP $http_code)"
else
    fail "Panel health endpoint did not respond (curl returned no HTTP code)"
fi
echo

# ── 7. nginx stub page ────────────────────────────────────────────────────────
echo "[ nginx stub ]"
stub_body=$(curl -s --max-time 5 http://127.0.0.1:80/ 2>/dev/null || true)
if [ -n "$stub_body" ]; then
    ok "nginx stub page returned a non-empty response"
else
    fail "nginx stub page returned empty or no response"
fi
echo

# ── Summary ───────────────────────────────────────────────────────────────────
echo "=== Summary ==="
echo "  Passed : $PASS"
echo "  Failed : $FAIL"
echo

if [ "$FAIL" -gt 0 ]; then
    echo -e "${red}Smoke test FAILED — $FAIL check(s) did not pass.${reset}"
    exit 1
else
    echo -e "${green}Smoke test PASSED — all checks OK.${reset}"
    exit 0
fi
