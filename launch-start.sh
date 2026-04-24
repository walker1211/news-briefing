#!/bin/bash
[ -n "${BASH_VERSION:-}" ] || exec /bin/bash "$0" "$@"
set -euo pipefail
cd "$(dirname "$0")"

PLIST="$HOME/Library/LaunchAgents/com.news-briefing.briefing.plist"
LABEL="com.news-briefing.briefing"
DOMAIN="gui/$(id -u)"
STATUS_PATTERN="^[[:space:]]+(state =|pid =|program =|working directory =|stdout path =|stderr path =|runs =|last terminating signal =)"

mkdir -p logs

if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    launchctl bootout "$DOMAIN" "$PLIST"
fi
launchctl bootstrap "$DOMAIN" "$PLIST"
launchctl kickstart -k "$DOMAIN/$LABEL"

status_output="$(launchctl print "$DOMAIN/$LABEL")" || {
    echo "launchd service status is unavailable after start: $DOMAIN/$LABEL" >&2
    exit 1
}

echo "launchd service started: $DOMAIN/$LABEL"
if ! grep -E "$STATUS_PATTERN" <<<"$status_output"; then
    echo "No key status fields found in launchctl output."
fi
