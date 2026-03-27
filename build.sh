#!/bin/bash
cd "$(dirname "$0")" || exit
echo "Building..."
go build -o news-briefing ./cmds/news-briefing/
echo "Done. Binary: ./news-briefing"
