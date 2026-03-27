#!/bin/bash
PID=$(pgrep -f "news-briefing serve")

if [ -z "$PID" ]; then
    echo "news-briefing serve is not running"
    exit 0
fi

kill "$PID"
echo "news-briefing serve stopped (PID: $PID)"
