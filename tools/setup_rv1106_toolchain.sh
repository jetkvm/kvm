#!/bin/bash
# tools/setup_rv1106_toolchain.sh
# Clone the rv1106-system toolchain to $HOME/.jetkvm/rv1106-system if not already present
set -e
JETKVM_HOME="$HOME/.jetkvm"
TOOLCHAIN_DIR="$JETKVM_HOME/rv1106-system"
REPO_URL="https://github.com/jetkvm/rv1106-system.git"

mkdir -p "$JETKVM_HOME"
if [ ! -d "$TOOLCHAIN_DIR" ]; then
  echo "Cloning rv1106-system toolchain to $TOOLCHAIN_DIR ..."
  git clone --depth 1 "$REPO_URL" "$TOOLCHAIN_DIR"
else
  echo "Toolchain already present at $TOOLCHAIN_DIR"
fi
