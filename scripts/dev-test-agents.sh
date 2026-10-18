#!/usr/bin/env bash
# Spin up tiny Alpine "devices" that run the real agent against the dev stack.
#
# Why: the dashboard needs devices to show telemetry, services, packages and
# logs, but running a full VM per device costs RAM we do not have. An Alpine
# container with the agent is a few megabytes and exercises the same enrollment,
# heartbeat and telemetry paths as a real machine.
#
# What it does, per container:
#   1. signs in to the dev server and creates a single-use enrollment token
#   2. enrolls a device with a stable machine_id (re-runs adopt the same record)
#   3. starts alpine + the agent binary served by GET /api/agent/download
#
# The binary is static (CGO_ENABLED=0), so no libc packages are needed. The
# container runs as root only because the agent's collectors prefer /proc and
# /sys; nothing outside the container is mounted.
#
# Usage:
#   scripts/dev-test-agents.sh            # create/refresh two devices
#   scripts/dev-test-agents.sh down       # remove them (device records stay)
#
# Requires: curl, jq, docker, and a running dev stack (`make dev-d`).
# Defaults can be overridden:
#   SYNCWIN_SERVER=http://localhost:8088
#   SYNCWIN_EMAIL=demo@sync-win.local
#   SYNCWIN_PASSWORD=sync-win-demo-password
#   AGENTS=2  IMAGE=alpine:3.24  MEMORY=64m  CPUS=0.25

set -euo pipefail

SERVER="${SYNCWIN_SERVER:-http://localhost:8088}"
EMAIL="${SYNCWIN_EMAIL:-demo@sync-win.local}"
PASSWORD="${SYNCWIN_PASSWORD:-sync-win-demo-password}"

AGENTS="${AGENTS:-2}"
IMAGE="${IMAGE:-alpine:3.24}"
MEMORY="${MEMORY:-64m}"
CPUS="${CPUS:-0.25}"
PREFIX="${PREFIX:-sync-win-test-agent}"

# Where the agent reaches the API from inside the compose network, and the
# network itself. Both are derived from the running dev server container so a
# second stack or a renamed service still works.
SERVER_CONTAINER="${SERVER_CONTAINER:-sync-win-dev-server-1}"
AGENT_SERVER_URL="${AGENT_SERVER_URL:-http://server:8080}"
NETWORK="${NETWORK:-}"

usage() {
    cat <<'EOF'
Create lightweight Alpine containers running the real agent against the dev stack.

Usage:
  scripts/dev-test-agents.sh        create/refresh the devices
  scripts/dev-test-agents.sh down   remove the containers (device records stay)

Defaults can be overridden with environment variables:
  SYNCWIN_SERVER   http://localhost:8088
  SYNCWIN_EMAIL    demo@sync-win.local
  SYNCWIN_PASSWORD sync-win-demo-password
  AGENTS           2
  IMAGE            alpine:3.24
  MEMORY           64m
  CPUS             0.25
EOF
}

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

container_name() {
    printf '%s-%s' "$PREFIX" "$1"
}

remove_agents() {
    local i
    for i in $(seq 1 "$AGENTS"); do
        docker rm -f "$(container_name "$i")" >/dev/null 2>&1 || true
    done
    printf 'Removed %s test agent container(s). Their device records remain in the dashboard.\n' "$AGENTS"
}

case "${1:-up}" in
    up|"") ;;
    down|remove)
        remove_agents
        exit 0
        ;;
    -h|--help|help)
        usage
        exit 0
        ;;
    *)
        die "unknown argument: $1 (use 'up' or 'down')"
        ;;
esac

command -v curl >/dev/null || die "curl is required"
command -v jq >/dev/null || die "jq is required"
command -v docker >/dev/null || die "docker is required"

if [ -z "$NETWORK" ]; then
    NETWORK="$(docker inspect "$SERVER_CONTAINER" \
        --format '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' 2>/dev/null || true)"
fi
[ -n "$NETWORK" ] || die "could not find the dev network; is '$SERVER_CONTAINER' running? (set NETWORK=...)"

curl -fsS -o /dev/null "$SERVER/health" || die "dev server is not answering at $SERVER"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
JAR="$WORK/cookies.txt"

# Sign in. There is no default login in the product; the dev account is created
# by `make dev-account`.
login_body="$(jq -n --arg email "$EMAIL" --arg password "$PASSWORD" \
    '{email: $email, password: $password}')"
login_code="$(curl -sS -c "$JAR" -o /dev/null -w '%{http_code}' \
    -X POST "$SERVER/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "$login_body")"
if [ "$login_code" != "200" ]; then
    die "sign-in as $EMAIL failed (HTTP $login_code). Run 'make dev-account' or set SYNCWIN_EMAIL/SYNCWIN_PASSWORD."
fi

ARCH="$(uname -m)"
KERNEL="$(uname -r)"

for i in $(seq 1 "$AGENTS"); do
    NAME="$(container_name "$i")"
    HOSTNAME="alpine-test-$i"
    # Stable machine id so a re-run adopts the existing device instead of
    # leaving a duplicate behind in the dashboard.
    MACHINE_ID="sync-win-test-agent-$i"

    TOKEN="$(curl -sS -b "$JAR" -X POST "$SERVER/api/agent/enroll-token" | jq -r '.token // empty')"
    [ -n "$TOKEN" ] || die "could not create an enrollment token for $HOSTNAME"

    ENROLL_BODY="$(jq -n \
        --arg token "$TOKEN" \
        --arg hostname "$HOSTNAME" \
        --arg arch "$ARCH" \
        --arg kernel "$KERNEL" \
        --arg machine "$MACHINE_ID" \
        '{token: $token, hostname: $hostname, architecture: $arch,
          os: "Alpine Linux (container)", kernel: $kernel,
          agent_version: "dev", machine_id: $machine, fingerprint: $machine}')"

    RESPONSE="$(curl -sS -X POST "$SERVER/api/agent/enroll" \
        -H 'Content-Type: application/json' -d "$ENROLL_BODY")"
    DEVICE_ID="$(printf '%s' "$RESPONSE" | jq -r '.device_id // empty')"
    DEVICE_TOKEN="$(printf '%s' "$RESPONSE" | jq -r '.device_token // empty')"
    if [ -z "$DEVICE_ID" ] || [ -z "$DEVICE_TOKEN" ]; then
        die "enrollment for $HOSTNAME failed: $RESPONSE"
    fi

    docker rm -f "$NAME" >/dev/null 2>&1 || true
    docker run -d \
        --name "$NAME" \
        --hostname "$HOSTNAME" \
        --network "$NETWORK" \
        --restart unless-stopped \
        --memory "$MEMORY" \
        --memory-swap "$MEMORY" \
        --cpus "$CPUS" \
        -e "SERVER_URL=$AGENT_SERVER_URL" \
        -e "DEVICE_ID=$DEVICE_ID" \
        -e "DEVICE_TOKEN=$DEVICE_TOKEN" \
        --entrypoint /bin/sh \
        "$IMAGE" -c '
            set -e
            wget -q -O /usr/local/bin/sync-win-agent "$SERVER_URL/api/agent/download"
            chmod +x /usr/local/bin/sync-win-agent
            exec /usr/local/bin/sync-win-agent daemon \
                --server "$SERVER_URL" \
                --device-id "$DEVICE_ID" \
                --device-token "$DEVICE_TOKEN" \
                --interval 5s
        ' >/dev/null

    printf 'Enrolled %s (device %s) and started container %s\n' "$HOSTNAME" "$DEVICE_ID" "$NAME"
done

printf '\nTest devices started (alpine, %s RAM each, %s CPU each).\n' "$MEMORY" "$CPUS"
printf 'Sign in as %s; they appear as soon as the first heartbeat lands (a few seconds).\n' "$EMAIL"
printf 'Remove them with: %s down\n' "$0"
