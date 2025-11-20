#!/bin/bash
# .devcontainer/install_audio_deps.sh
# Build ALSA and Opus static libs for ARM in /opt/jetkvm-audio-libs
set -e

# Sudo wrapper function
function use_sudo() {
  if [ "$UID" -eq 0 ] || [ -z "$(which sudo 2>/dev/null)" ]; then
    "$@"
  else
    sudo -E "$@"
  fi
}

# Accept version parameters or use defaults
ALSA_VERSION="${1:-1.2.14}"
OPUS_VERSION="${2:-1.5.2}"

AUDIO_LIBS_DIR="/opt/jetkvm-audio-libs"
BUILDKIT_PATH="/opt/jetkvm-native-buildkit"
BUILDKIT_FLAVOR="arm-rockchip830-linux-uclibcgnueabihf"
CROSS_PREFIX="$BUILDKIT_PATH/bin/$BUILDKIT_FLAVOR"

# Create directory with proper permissions
use_sudo mkdir -p "$AUDIO_LIBS_DIR"
use_sudo chmod 777 "$AUDIO_LIBS_DIR"
cd "$AUDIO_LIBS_DIR"

# Download sources
[ -f alsa-lib-${ALSA_VERSION}.tar.bz2 ] || wget -N https://www.alsa-project.org/files/pub/lib/alsa-lib-${ALSA_VERSION}.tar.bz2
[ -f opus-${OPUS_VERSION}.tar.gz ] || wget -N https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz

# Extract
[ -d alsa-lib-${ALSA_VERSION} ] || tar xf alsa-lib-${ALSA_VERSION}.tar.bz2
[ -d opus-${OPUS_VERSION} ] || tar xf opus-${OPUS_VERSION}.tar.gz

# Optimization flags for ARM Cortex-A7 with NEON (simplified to avoid FD_SETSIZE issues)
OPTIM_CFLAGS="-O2 -mfpu=neon -mtune=cortex-a7 -mfloat-abi=hard"

export CC="${CROSS_PREFIX}-gcc"
export CFLAGS="$OPTIM_CFLAGS"
export CXXFLAGS="$OPTIM_CFLAGS"

# Build ALSA
cd alsa-lib-${ALSA_VERSION}
if [ ! -f .built ]; then
  chown -R $(whoami):$(whoami) .
  # Use minimal ALSA configuration to avoid FD_SETSIZE issues in devcontainer
  CFLAGS="$OPTIM_CFLAGS" ./configure --host $BUILDKIT_FLAVOR \
    --enable-static=yes --enable-shared=no \
    --with-pcm-plugins=rate,linear \
    --disable-seq --disable-rawmidi --disable-ucm \
    --disable-python --disable-old-symbols \
    --disable-topology --disable-hwdep --disable-mixer \
    --disable-alisp --disable-aload --disable-resmgr
  make -j$(nproc)
  touch .built
fi
cd ..

# Build Opus
cd opus-${OPUS_VERSION}
if [ ! -f .built ]; then
  chown -R $(whoami):$(whoami) .
  CFLAGS="$OPTIM_CFLAGS" ./configure --host $BUILDKIT_FLAVOR --enable-static=yes --enable-shared=no --enable-fixed-point
  make -j$(nproc)
  touch .built
fi
cd ..

echo "ALSA and Opus built in $AUDIO_LIBS_DIR"
