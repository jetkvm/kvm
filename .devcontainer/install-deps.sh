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

# Remove expired/problematic third-party repos that may cause apt-get update to fail
sudo rm -f /etc/apt/sources.list.d/yarn.list 2>/dev/null || true

# Update package lists (allow some failures for third-party repos)
sudo apt-get update || true

# Install required packages
sudo apt-get install -y --no-install-recommends \
  git \
  iputils-ping \
  openssh-client \
  build-essential \
  device-tree-compiler \
  gperf g++-multilib gcc-multilib \
  gdb-multiarch \
  libnl-3-dev libdbus-1-dev libelf-dev libmpc-dev dwarves \
  bc openssl flex bison libssl-dev python3 python-is-python3 texinfo kmod cmake \
  wget zstd \
  python3-venv python3-kconfiglib \
  protobuf-compiler

sudo rm -rf /var/lib/apt/lists/*

# Verify zstd is installed (required for buildkit extraction)
if ! command -v unzstd &> /dev/null; then
    echo "ERROR: zstd/unzstd not installed properly"
    exit 1
fi

# Install Go protobuf plugins for proto generation
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Install buildkit (cross-compiler for ARM)
BUILDKIT_VERSION="v0.2.7"
BUILDKIT_PATH="/opt/jetkvm-native-buildkit"
BUILDKIT_GCC="${BUILDKIT_PATH}/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc"

# Skip if buildkit already installed and working
if [ -x "${BUILDKIT_GCC}" ]; then
    echo "Buildkit already installed at ${BUILDKIT_PATH}"
else
    echo "Installing buildkit ${BUILDKIT_VERSION}..."
    BUILDKIT_TMPDIR="$(mktemp -d)"
    pushd "${BUILDKIT_TMPDIR}" > /dev/null

    wget -q --show-progress https://github.com/jetkvm/rv1106-system/releases/download/${BUILDKIT_VERSION}/buildkit.tar.zst
    sudo rm -rf "${BUILDKIT_PATH}"
    sudo mkdir -p "${BUILDKIT_PATH}"
    sudo tar --use-compress-program="unzstd --long=31" -xf buildkit.tar.zst -C "${BUILDKIT_PATH}"
    rm buildkit.tar.zst

    popd
    rm -rf "${BUILDKIT_TMPDIR}"

    # Verify installation
    if [ ! -x "${BUILDKIT_GCC}" ]; then
        echo "ERROR: Buildkit installation failed - gcc not found at ${BUILDKIT_GCC}"
        exit 1
    fi
    echo "Buildkit installed successfully"
fi
