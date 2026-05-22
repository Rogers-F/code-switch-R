#!/bin/bash

# Ensure ~/go/bin is in PATH so wails3 can be found
export PATH="$PATH:$HOME/go/bin"

# Navigate to the script's directory to ensure we run in the project root
cd "$(dirname "$0")"

echo "🚀 Starting Code Switch R development environment..."
wails3 task dev
