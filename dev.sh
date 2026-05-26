#!/bin/bash

# Ensure ~/go/bin is in PATH so wails3 can be found
export PATH="$PATH:$HOME/go/bin"

# Navigate to the script's directory to ensure we run in the project root
cd "$(dirname "$0")"

# Check if port 9245 is already in use, and clean it up if needed
PORT=9245
PID=$(lsof -t -i:$PORT)
if [ -n "$PID" ]; then
  echo "⚠️ Port $PORT is already in use by process $PID. Cleaning it up..."
  kill -9 $PID
  sleep 1
fi

echo "🚀 Starting Code Switch R development environment..."
wails3 task dev

