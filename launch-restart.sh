#!/bin/bash
cd "$(dirname "$0")" || exit
./launch-stop.sh 2>/dev/null || true
./launch-start.sh
