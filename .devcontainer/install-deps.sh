#!/bin/bash

SUDO_PATH=$(which sudo)
function sudo() {
  if [ "$UID" -eq 0 ]; then
    "$@"
  else
    ${SUDO_PATH} "$@"
  fi
}

set -ex

export DEBIAN_FRONTEND=noninteractive
sudo apt-get update && \
sudo apt-get install -y --no-install-recommends \
  build-essential \
  device-tree-compiler \
  gperf g++-multilib gcc-multilib \
  libnl-3-dev libdbus-1-dev libelf-dev libmpc-dev dwarves \
  bc openssl flex bison libssl-dev python3 python-is-python3 texinfo kmod cmake \
  wget zstd \
  python3-venv python3-kconfiglib \
  && sudo rm -rf /var/lib/apt/lists/*

# Install buildkit
BUILDKIT_VERSION="v0.2.5"
BUILDKIT_TMPDIR="$(mktemp -d)"
pushd "${BUILDKIT_TMPDIR}" > /dev/null

wget https://github.com/jetkvm/rv1106-system/releases/download/${BUILDKIT_VERSION}/buildkit.tar.zst && \
    sudo mkdir -p /opt/jetkvm-native-buildkit && \
    sudo tar --use-compress-program="unzstd --long=31" -xvf buildkit.tar.zst -C /opt/jetkvm-native-buildkit && \
    rm buildkit.tar.zst
popd