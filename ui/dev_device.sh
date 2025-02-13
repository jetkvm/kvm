#!/bin/bash

# Print header
echo "┌──────────────────────────────────────┐"
echo "│     JetKVM Development Setup         │"
echo "└──────────────────────────────────────┘"

# Prompt for IP address
printf "Please enter the IP address of your JetKVM device: "
read ip_address

# Validate input is not empty
if [ -z "$ip_address" ]; then
    echo "Error: IP address cannot be empty"
    exit 1
fi

# Set the environment variable and run Vite
echo "Starting development server with JetKVM device at: $ip_address"
sleep 1
JETKVM_PROXY_URL="http://$ip_address" vite dev --mode=device
