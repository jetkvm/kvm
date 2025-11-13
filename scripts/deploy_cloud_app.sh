#!/bin/bash
set -e

# Get current branch name
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

# Verify branch name matches release/x.x.x or release/x.x.x-dev...
if [[ ! $CURRENT_BRANCH =~ ^release/[0-9]+\.[0-9]+\.[0-9]+(-dev[0-9]+)?$ ]]; then
  echo "✗ Error: Current branch '$CURRENT_BRANCH' does not match required pattern"
  echo "  Expected: release/x.x.x OR release/x.x.x-dev20241104123632"
  exit 1
fi

# Extract version from branch name (remove "release/" prefix)
VERSION=${CURRENT_BRANCH#release/}

echo "Current branch: $CURRENT_BRANCH"
echo "Version: $VERSION"
echo ""

# Change to ui directory
cd ui

# Ask for confirmation
read -p "Do you want to deploy the cloud app to production? (y/N): " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Deployment cancelled."
  exit 0
fi

# Build for root dist
echo ""
echo "Building for root dist..."
npm ci
npm run build:prod

# Build for versioned dist/v/VERSION
echo ""
echo "Building for dist/v/${VERSION}..."
npm ci
npm run build:prod -- --base=/v/${VERSION}/ --outDir dist/v/${VERSION}

# Deploy to production
echo ""
echo "Deploying to r2://jetkvm-cloud-app..."
rclone copyto dist r2://jetkvm-cloud-app

echo ""
echo "✓ Successfully deployed v${VERSION} to production"
