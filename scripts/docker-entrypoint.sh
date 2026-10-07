#!/bin/sh
# Container entrypoint.
#
# The image ships an unprivileged runtime user, but a database created by an
# earlier root-running image is owned by root with mode 0644, so the server
# cannot open it: "attempt to write a readonly database". Upgrading such an
# install without this step would take the service down.
#
# The container therefore starts as root only to fix ownership of the data
# directory, then replaces itself with the server running as the unprivileged
# user. `exec` keeps the server as PID 1 so SIGTERM reaches it directly and
# graceful shutdown still works; a shell left in front would swallow it.
set -e

DATA_DIR="${SYNCWIN_DATA_DIR:-/data}"
APP_USER="syncwin"
APP_GROUP="syncwin"

# If someone started the container with an explicit non-root user, there is
# nothing to fix and nothing to drop privileges from.
if [ "$(id -u)" != "0" ]; then
	exec /app/server
fi

# Only touch ownership when it is actually wrong, so a read-only or already
# correct mount is left alone.
if [ -d "$DATA_DIR" ] && [ "$(stat -c '%u:%g' "$DATA_DIR")" != "10001:10001" ]; then
	chown -R "$APP_USER:$APP_GROUP" "$DATA_DIR"
fi

# su-exec is a static setuid-free helper that execs without a login shell and
# without the signal problems a shell wrapper would introduce.
exec su-exec "$APP_USER" /app/server