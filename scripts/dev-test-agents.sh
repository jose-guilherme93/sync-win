#!/usr/bin/env bash
# Create test "devices" that run the real agent against the dev stack.
#
# Two kinds, because they answer different questions:
#
#   alpine (default)  Two ~4 MB Alpine containers. Cheap enough to leave running
#                     and enough to prove enrollment, heartbeats and telemetry.
#                     They have no systemd, journald or apt, so Services, Logs
#                     and Packages stay empty by design.
#
#   full              One Ubuntu container booting systemd as PID 1, with
#                     journald, apt, nginx (a listening port), Lynis, sshd and
#                     the host's Docker socket. This is the device that exercises
#                     Services, Logs, Packages, Security and Docker, and it is
#                     the only one that can open real Remote access sessions:
#                     the policy allows the `devuser` account and the helper is
#                     installed. It needs ~100 MB of RAM and is privileged; it
#                     is development-only.
#
# Both kinds enroll through the same one-time token flow and reuse a stable
# machine_id, so re-running adopts the existing device instead of duplicating it.
#
# Usage:
#   scripts/dev-test-agents.sh            # two Alpine devices
#   scripts/dev-test-agents.sh full       # one systemd device
#   scripts/dev-test-agents.sh all        # both
#   scripts/dev-test-agents.sh down       # remove every test container
#
# Requires: curl, jq, docker, and a running dev stack (`make dev-d`).
# Defaults can be overridden:
#   SYNCWIN_SERVER=http://localhost:8088
#   SYNCWIN_EMAIL=demo@sync-win.local
#   SYNCWIN_PASSWORD=sync-win-demo-password
#   AGENTS=2  IMAGE=alpine:3.24  MEMORY=64m  CPUS=0.25
#   FULL_IMAGE=sync-win-test-device:24.04  FULL_MEMORY=256m  FULL_CPUS=0.5

set -euo pipefail

# Build paths are relative to the repository, not to the caller's cwd.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SERVER="${SYNCWIN_SERVER:-http://localhost:8088}"
EMAIL="${SYNCWIN_EMAIL:-demo@sync-win.local}"
PASSWORD="${SYNCWIN_PASSWORD:-sync-win-demo-password}"

AGENTS="${AGENTS:-2}"
IMAGE="${IMAGE:-alpine:3.24}"
MEMORY="${MEMORY:-64m}"
CPUS="${CPUS:-0.25}"
PREFIX="${PREFIX:-sync-win-test-agent}"

FULL_IMAGE="${FULL_IMAGE:-sync-win-test-device:24.04}"
FULL_MEMORY="${FULL_MEMORY:-256m}"
FULL_CPUS="${FULL_CPUS:-0.5}"
FULL_NAME="${PREFIX}-full"
FULL_HOSTNAME="${FULL_HOSTNAME:-ubuntu-full-1}"
DOCKERFILE="${DOCKERFILE:-docker/Dockerfile.test-device}"

# Where the agent reaches the API from inside the compose network, and the
# network itself. Both are derived from the running dev server container so a
# second stack or a renamed service still works.
SERVER_CONTAINER="${SERVER_CONTAINER:-sync-win-dev-server-1}"
AGENT_SERVER_URL="${AGENT_SERVER_URL:-http://server:8080}"
NETWORK="${NETWORK:-}"

usage() {
    cat <<'EOF'
Create test devices running the real agent against the dev stack.

Usage:
  scripts/dev-test-agents.sh        two lightweight Alpine devices
  scripts/dev-test-agents.sh full   one systemd Ubuntu device (Services/Logs/apt/Docker/Lynis)
  scripts/dev-test-agents.sh all    both kinds
  scripts/dev-test-agents.sh down   remove every test container (records stay)

Environment overrides:
  SYNCWIN_SERVER   http://localhost:8088
  SYNCWIN_EMAIL    demo@sync-win.local
  SYNCWIN_PASSWORD sync-win-demo-password
  AGENTS           2
  IMAGE            alpine:3.24
  MEMORY           64m
  CPUS             0.25
  FULL_IMAGE       sync-win-test-device:24.04
  FULL_MEMORY      256m
  FULL_CPUS        0.5
EOF
}

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

container_name() {
    printf '%s-%s' "$PREFIX" "$1"
}

# enroll <hostname> <machine-id> <os> — sets DEVICE_ID and DEVICE_TOKEN.
enroll() {
    local hostname="$1" machine_id="$2" os="$3" token response

    token="$(curl -sS -b "$JAR" -X POST "$SERVER/api/agent/enroll-token" | jq -r '.token // empty')"
    [ -n "$token" ] || die "could not create an enrollment token for $hostname"

    response="$(curl -sS -X POST "$SERVER/api/agent/enroll" \
        -H 'Content-Type: application/json' \
        -d "$(jq -n \
            --arg token "$token" \
            --arg hostname "$hostname" \
            --arg arch "$ARCH" \
            --arg kernel "$KERNEL" \
            --arg os "$os" \
            --arg machine "$machine_id" \
            '{token: $token, hostname: $hostname, architecture: $arch,
              os: $os, kernel: $kernel, agent_version: "dev",
              machine_id: $machine, fingerprint: $machine}')")"

    DEVICE_ID="$(printf '%s' "$response" | jq -r '.device_id // empty')"
    DEVICE_TOKEN="$(printf '%s' "$response" | jq -r '.device_token // empty')"
    if [ -z "$DEVICE_ID" ] || [ -z "$DEVICE_TOKEN" ]; then
        die "enrollment for $hostname failed: $response"
    fi
}

start_alpine() {
    local i name hostname
    # Stable machine id so a re-run adopts the existing device instead of
    # leaving a duplicate behind in the dashboard.
    for i in $(seq 1 "$AGENTS"); do
        name="$(container_name "$i")"
        hostname="alpine-test-$i"
        enroll "$hostname" "sync-win-test-agent-$i" "Alpine Linux (container)"

        docker rm -f "$name" >/dev/null 2>&1 || true
        docker run -d \
            --name "$name" \
            --hostname "$hostname" \
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

        printf 'Enrolled %s (device %s) and started container %s\n' "$hostname" "$DEVICE_ID" "$name"
    done
}

wait_for_systemd() {
    local name="$1" state i
    for i in $(seq 1 60); do
        state="$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)"
        case "$state" in
            running|degraded) return 0 ;;
        esac
        sleep 1
    done
    printf 'systemd did not come up in %s; last state: %s\n' "$name" "${state:-unknown}" >&2
    docker logs --tail=30 "$name" >&2 || true
    return 1
}

start_full() {
    enroll "$FULL_HOSTNAME" "sync-win-test-agent-full" "Ubuntu 24.04 (container)"

    # Rebuild on every run. The layer cache makes an unchanged image cheap, and
    # a changed Dockerfile must not be ignored just because an image from an
    # older build already exists (that silently kept sshd out of this image).
    printf 'Building %s from %s (cached layers make this fast)...\n' "$FULL_IMAGE" "$DOCKERFILE"
    docker build -f "$DOCKERFILE" -t "$FULL_IMAGE" . >/dev/null

    docker rm -f "$FULL_NAME" >/dev/null 2>&1 || true
    # systemd needs to be PID 1 and to own the cgroup hierarchy; journald and
    # the Docker socket mount are what make the Logs and Docker screens real.
    # The host socket is mounted read-write because the Docker API needs it;
    # mutations are still gated by SYNCWIN_ENABLE_DOCKER_MUTATIONS on the server.
    docker run -d \
        --name "$FULL_NAME" \
        --hostname "$FULL_HOSTNAME" \
        --network "$NETWORK" \
        --privileged \
        --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
        -v /var/run/docker.sock:/var/run/docker.sock \
        --tmpfs /run \
        --tmpfs /run/lock \
        --restart unless-stopped \
        --memory "$FULL_MEMORY" \
        --memory-swap "$FULL_MEMORY" \
        --cpus "$FULL_CPUS" \
        "$FULL_IMAGE" >/dev/null

    wait_for_systemd "$FULL_NAME" || die "the full test device failed to boot"

    # The agent runs inside systemd so a restart of the container brings it back
    # with the same credentials.
    cat > "$WORK/sync-win-agent.service" <<EOF
[Unit]
Description=SyncWin dev agent
After=network-online.target docker.socket
Wants=network-online.target

[Service]
Type=simple
# The collectors expand $HOME for the preference/save paths; without it every
# cycle fails with "home is required". StateDirectory gives the agent a stable
# state file, as the real unit does.
Environment=HOME=/root
StateDirectory=sync-win
ExecStart=/usr/local/bin/sync-win-agent daemon --server $AGENT_SERVER_URL --device-id $DEVICE_ID --device-token $DEVICE_TOKEN --interval 5s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    # The remote-access helper is the root half of an interactive session: it
    # injects the ephemeral key and nothing else. The dev agent runs as root, so
    # the helper answers root's uid; on a real device it answers the sync-win
    # service user.
    cat > "$WORK/sync-win-agent-helper.service" <<'EOF'
[Unit]
Description=SyncWin dev remote-access helper
After=local-fs.target
Before=sync-win-agent.service

[Service]
Type=simple
ExecStart=/usr/local/bin/sync-win-agent helper --user root --group root
Restart=always
RestartSec=5
RuntimeDirectory=sync-win
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
EOF

    # A unit that exits non-zero on purpose: the Services screen needs something
    # in the Failed tile to be worth looking at.
    cat > "$WORK/demo-failure.service" <<'EOF'
[Unit]
Description=SyncWin demo failing unit

[Service]
Type=oneshot
ExecStart=/bin/false

[Install]
WantedBy=multi-user.target
EOF

    docker exec "$FULL_NAME" sh -c \
        "curl -fsSL '$AGENT_SERVER_URL/api/agent/download' -o /usr/local/bin/sync-win-agent && chmod +x /usr/local/bin/sync-win-agent"
    docker cp "$WORK/sync-win-agent.service" "$FULL_NAME:/etc/systemd/system/sync-win-agent.service"
    docker cp "$WORK/sync-win-agent-helper.service" "$FULL_NAME:/etc/systemd/system/sync-win-agent-helper.service"
    docker cp "$WORK/demo-failure.service" "$FULL_NAME:/etc/systemd/system/demo-failure.service"
    docker exec "$FULL_NAME" systemctl daemon-reload
    docker exec "$FULL_NAME" systemctl enable --now sync-win-agent-helper
    # Local consent, exactly as the owner would grant it on a real machine.
    # `set` writes the full fail-closed policy with only remote access enabled.
    docker exec "$FULL_NAME" /usr/local/bin/sync-win-agent set ssh on
    docker exec "$FULL_NAME" /usr/local/bin/sync-win-agent set ssh-user devuser
    docker exec "$FULL_NAME" systemctl enable --now ssh
    docker exec "$FULL_NAME" systemctl enable --now sync-win-agent
    docker exec "$FULL_NAME" systemctl enable --now nginx
    # This one is expected to fail; do not let `set -e` abort the setup.
    docker exec "$FULL_NAME" systemctl enable --now demo-failure.service >/dev/null 2>&1 || true

    printf 'Enrolled %s (device %s) and started container %s\n' "$FULL_HOSTNAME" "$DEVICE_ID" "$FULL_NAME"
    printf '  systemd units: nginx running, demo-failure failed, sync-win-agent running\n'
    printf '  remote access: sshd running, helper running, policy allows devuser\n'
}

remove_agents() {
    local i
    for i in $(seq 1 "$AGENTS"); do
        docker rm -f "$(container_name "$i")" >/dev/null 2>&1 || true
    done
    docker rm -f "$FULL_NAME" >/dev/null 2>&1 || true
    printf 'Removed the test agent containers. Their device records remain in the dashboard.\n'
}

MODE="${1:-up}"
case "$MODE" in
    up|alpine|"") MODE="alpine" ;;
    full|all) ;;
    down|remove)
        remove_agents
        exit 0
        ;;
    -h|--help|help)
        usage
        exit 0
        ;;
    *)
        die "unknown argument: $1 (use 'up', 'full', 'all' or 'down')"
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

# Sign in. The dev stack bootstraps this fixed account and injects
# SYNCWIN_DEV_TOKEN; the same password still works through the normal form.
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

case "$MODE" in
    alpine) start_alpine ;;
    full) start_full ;;
    all) start_alpine; start_full ;;
esac

printf '\nTest devices ready. Sign in as %s; they appear after the first heartbeat.\n' "$EMAIL"
printf 'Remove them with: %s down\n' "$0"
