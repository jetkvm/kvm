#!/bin/bash
set -e

# Build latest (includes device list)
echo "Building latest..."
VITE_BASE_PATH=/v/latest/ npm run build:prod -- --outDir dist-temp
mkdir -p dist/v/latest
cp -r dist-temp/* dist/v/latest/

# Build for each firmware version you support
echo "Building v2025.11.07..."
VITE_BASE_PATH=/v/2025.11.07/ npm run build:prod -- --outDir dist-temp
mkdir -p dist/v/2025.11.07
cp -r dist-temp/* dist/v/2025.11.07/

echo "Building v2025.10.15..."
VITE_BASE_PATH=/v/2025.10.15/ npm run build:prod -- --outDir dist-temp
mkdir -p dist/v/2025.10.15
cp -r dist-temp/* dist/v/2025.10.15/

rm -rf dist-temp
echo "✓ All versions built to dist/v/"

