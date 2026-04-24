#!/bin/bash
[ -n "${BASH_VERSION:-}" ] || exec /bin/bash "$0" "$@"
set -euo pipefail
cd "$(dirname "$0")"

LABEL="com.news-briefing.briefing"
DOMAIN="gui/$(id -u)"
STATUS_PATTERN="^[[:space:]]+(state =|pid =|program =|working directory =|stdout path =|stderr path =|runs =|last terminating signal =)"

echo "launchd service status: $DOMAIN/$LABEL"
if ! status_output="$(launchctl print "$DOMAIN/$LABEL" 2>&1)"; then
    echo "launchd service is not loaded: $DOMAIN/$LABEL" >&2
    exit 1
fi

if ! grep -E "$STATUS_PATTERN" <<<"$status_output"; then
    echo "No key status fields found in launchctl output."
fi
