#!/bin/bash
cd "$(dirname "$0")" || exit
./stop.sh && ./start.sh
