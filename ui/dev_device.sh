#!/usr/bin/env bash

# Check if an IP address was provided as an argument
if [ -z "$1" ]; then
    echo "Usage: $0 <JetKVM IP Address>"
    exit 1
fi

ip_address="$1"

# Print header
echo "┌──────────────────────────────────────┐"
echo "│     JetKVM Development Setup         │"
echo "└──────────────────────────────────────┘"

# Set the environment variable and run Vite
echo "Starting development server with JetKVM device at: $ip_address"

# Check if pwd is the current directory of the script
if [ "$(pwd)" != "$(dirname "$0")" ]; then
    pushd "$(dirname "$0")" > /dev/null
    echo "Changed directory to: $(pwd)"
fi

sleep 1

JETKVM_PROXY_URL="ws://$ip_address" npx vite dev --mode=device

popd > /dev/null
