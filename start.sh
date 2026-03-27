#!/bin/bash
cd "$(dirname "$0")" || exit

if pgrep -f "news-briefing serve" > /dev/null; then
    echo "news-briefing serve is already running (PID: $(pgrep -f 'news-briefing serve'))"
    exit 1
fi

mkdir -p logs
nohup ./news-briefing serve > logs/out.log 2>&1 &
echo "news-briefing serve started (PID: $!)"
echo "Logs: logs/out.log"
