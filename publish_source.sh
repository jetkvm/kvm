#!/usr/bin/env bash

# Check if a commit message was provided
if [ -z "$1" ]; then
    echo "Usage: $0 \"Your commit message here\""
    exit 1
fi

COMMIT_MESSAGE="$1"

# Ensure you're on the main branch
git checkout main

# Add 'public' remote if it doesn't exist
if ! git remote | grep -q '^public$'; then
    git remote add public https://github.com/jetkvm/kvm.git
fi

# Fetch the latest from the public repository
git fetch public || true

# Create a temporary branch for the release
git checkout -b release-temp

# If public/main exists, reset to it; else, use the root commit
if git ls-remote --heads public main | grep -q 'refs/heads/main'; then
    git reset --soft public/main
else
    git reset --soft "$(git rev-list --max-parents=0 HEAD)"
fi

# Merge changes from main
git merge --squash main

# Commit all changes as a single release commit
git commit -m "$COMMIT_MESSAGE"

# Force push the squashed commit to the public repository
git push --force public release-temp:main

# Switch back to main and delete the temporary branch
git checkout main
git branch -D release-temp

# Remove the public remote
git remote remove public
