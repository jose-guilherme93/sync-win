#!/usr/bin/env bash
# Tests for the installer's remote-session guard.
#
# These are the cases that were missing when a maintenance install was started
# from a SyncWin remote terminal: it stopped the agent that carried the
# terminal, the session died, and the rollback deleted the unit. A device whose
# only way in was that terminal could not be reached again.
#
# The functions are exercised by sourcing install.sh, which installs nothing
# when it is sourced.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FAILURES=0

# shellcheck source=/dev/null
source "$HERE/install.sh"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    FAILURES=$((FAILURES + 1))
}

pass() {
    printf 'ok: %s\n' "$*"
}

assert_true() {
    local description="$1"
    shift
    if "$@"; then pass "$description"; else fail "$description"; fi
}

assert_false() {
    local description="$1"
    shift
    if "$@" >/dev/null 2>&1; then fail "$description"; else pass "$description"; fi
}

# --- install_must_detach -----------------------------------------------------

unset SYNCWIN_REMOTE_SESSION SYNCWIN_DETACHED
assert_false "a local install runs in the foreground" install_must_detach

SYNCWIN_REMOTE_SESSION="ticket-123"
assert_true "an install inside a remote session detaches" install_must_detach

SYNCWIN_DETACHED=1
assert_false "the detached run does not detach again" install_must_detach

SYNCWIN_DETACHED=0
assert_true "any value other than 1 does not count as detached" install_must_detach
unset SYNCWIN_REMOTE_SESSION SYNCWIN_DETACHED

# --- install_detach_argv -----------------------------------------------------

SYNCWIN_SERVER="http://sync-win.local:8080"
SYNCWIN_TOKEN="tok-abc"
SUDO_USER="alice"
mapfile -t argv < <(install_detach_argv "/run/syncwin-install-1.sh" "syncwin-install-1")

[ "${argv[0]}" = "systemd-run" ] \
    && pass "the detached command is systemd-run" \
    || fail "the detached command is systemd-run"

grep -qx -- "--collect" <<<"$(printf '%s\n' "${argv[@]}")" \
    && pass "the transient unit is collected after it ends" \
    || fail "the transient unit is collected after it ends"

grep -qx -- "--unit=syncwin-install-1" <<<"$(printf '%s\n' "${argv[@]}")" \
    && pass "the unit is named so journalctl can follow it" \
    || fail "the unit is named so journalctl can follow it"

grep -qx -- "--setenv=SYNCWIN_DETACHED=1" <<<"$(printf '%s\n' "${argv[@]}")" \
    && pass "the detached run is marked as detached" \
    || fail "the detached run is marked as detached"

grep -qx -- "--setenv=SUDO_USER=alice" <<<"$(printf '%s\n' "${argv[@]}")" \
    && pass "the operator's account still becomes the remote-access target" \
    || fail "the operator's account still becomes the remote-access target"

[ "${argv[${#argv[@]} - 2]}" = "bash" ] && [ "${argv[${#argv[@]} - 1]}" = "/run/syncwin-install-1.sh" ] \
    && pass "the staged installer is the command, not a shell string" \
    || fail "the staged installer is the command, not a shell string"

# A hostile value must stay one argv element, never become shell input.
SYNCWIN_SERVER='http://x; touch /tmp/pwned'
mapfile -t argv < <(install_detach_argv "/run/i.sh" "u")
if printf '%s\n' "${argv[@]}" | grep -qxF -- "--setenv=SYNCWIN_SERVER=$SYNCWIN_SERVER"; then
    pass "a semicolon in a value stays inside one argument"
else
    fail "a semicolon in a value stays inside one argument"
fi

# --- install_detach_env_safe -------------------------------------------------

SYNCWIN_SERVER="http://ok"
assert_true "single-line values are accepted" install_detach_env_safe

SYNCWIN_SERVER=$'http://ok\n--unit=evil'
assert_false "a newline in a value is refused" install_detach_env_safe

SYNCWIN_SERVER=$'http://ok\r--unit=evil'
assert_false "a carriage return in a value is refused" install_detach_env_safe

unset SYNCWIN_SERVER SYNCWIN_TOKEN SUDO_USER

# --- the install cannot be driven by the marker ------------------------------
#
# The marker only changes how the installer runs. It must not be usable to skip
# a step, and it must not appear in the detach list of privileges.

grep -q 'SYNCWIN_REMOTE_SESSION' "$HERE/install.sh" \
    && pass "the installer reads the session marker" \
    || fail "the installer reads the session marker"

if grep -n 'SYNCWIN_REMOTE_SESSION' "$HERE/install.sh" | grep -qv 'install_must_detach\|^\s*#'; then
    # Any other use means the marker reached logic that changes what is installed.
    if grep -n 'SYNCWIN_REMOTE_SESSION' "$HERE/install.sh" | grep -q 'step\|exit 0'; then
        fail "the session marker must not bypass install steps"
    else
        pass "the session marker does not bypass install steps"
    fi
else
    pass "the session marker is read only by the guard"
fi

printf '\n'
if [ "$FAILURES" -gt 0 ]; then
    printf '%d installer guard test(s) failed\n' "$FAILURES" >&2
    exit 1
fi
printf 'installer guard tests passed\n'
