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
ARCH="$(dpkg --print-architecture)"
APT_PACKAGES=(
  iputils-ping
  build-essential
  device-tree-compiler
  gperf
  gdb-multiarch
  libnl-3-dev
  libdbus-1-dev
  libelf-dev
  libmpc-dev
  dwarves
  bc
  openssl
  flex
  bison
  libssl-dev
  python3
  python-is-python3
  texinfo
  kmod
  cmake
  wget
  zstd
  python3-venv
  python3-kconfiglib
  ripgrep
  ca-certificates
  curl
  gnupg
  nodejs
  npm
)

if [ "${ARCH}" = "amd64" ]; then
  APT_PACKAGES+=(g++-multilib gcc-multilib)
else
  echo "Skipping gcc/g++ multilib packages on ${ARCH}."
fi

sudo apt-get update && \
    sudo apt-get install -y --no-install-recommends "${APT_PACKAGES[@]}" && \
    sudo rm -rf /var/lib/apt/lists/*

# Install buildkit
BUILDKIT_VERSION="v0.2.5"
BUILDKIT_TMPDIR="$(mktemp -d)"
pushd "${BUILDKIT_TMPDIR}" > /dev/null

wget https://github.com/jetkvm/rv1106-system/releases/download/${BUILDKIT_VERSION}/buildkit.tar.zst && \
    sudo mkdir -p /opt/jetkvm-native-buildkit && \
    sudo tar --use-compress-program="unzstd --long=31" -xvf buildkit.tar.zst -C /opt/jetkvm-native-buildkit && \
    rm buildkit.tar.zst
popd
rm -rf "${BUILDKIT_TMPDIR}"

# Playwright Chromium system libraries (libnspr4, libnss3, libgbm, X11/xcb, etc.)
# Needed for `make test_e2e` / `npx playwright test` to launch the bundled headless shell.
sudo env "PATH=$PATH" npm exec --yes playwright@latest install-deps chromium

# Docker CLI
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian \
  trixie stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update && \
  sudo apt-get install -y docker-ce-cli && \
  sudo rm -rf /var/lib/apt/lists/*
