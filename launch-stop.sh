#!/bin/bash
cd "$(dirname "$0")" || exit

PLIST="$HOME/Library/LaunchAgents/com.news-briefing.briefing.plist"
DOMAIN="gui/$(id -u)"

launchctl bootout "$DOMAIN" "$PLIST"
