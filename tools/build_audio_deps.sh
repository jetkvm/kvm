#!/bin/bash
# tools/build_audio_deps.sh
# Build ALSA and Opus static libs for ARM in $HOME/.jetkvm/audio-libs
set -e

# Accept version parameters or use defaults
ALSA_VERSION="${1:-1.2.14}"
OPUS_VERSION="${2:-1.5.2}"

JETKVM_HOME="$HOME/.jetkvm"
AUDIO_LIBS_DIR="$JETKVM_HOME/audio-libs"
TOOLCHAIN_DIR="$JETKVM_HOME/rv1106-system"
CROSS_PREFIX="$TOOLCHAIN_DIR/tools/linux/toolchain/arm-rockchip830-linux-uclibcgnueabihf/bin/arm-rockchip830-linux-uclibcgnueabihf"

mkdir -p "$AUDIO_LIBS_DIR"
cd "$AUDIO_LIBS_DIR"

# Download sources
[ -f alsa-lib-${ALSA_VERSION}.tar.bz2 ] || wget -N https://www.alsa-project.org/files/pub/lib/alsa-lib-${ALSA_VERSION}.tar.bz2
[ -f opus-${OPUS_VERSION}.tar.gz ] || wget -N https://downloads.xiph.org/releases/opus/opus-${OPUS_VERSION}.tar.gz

# Extract
[ -d alsa-lib-${ALSA_VERSION} ] || tar xf alsa-lib-${ALSA_VERSION}.tar.bz2
[ -d opus-${OPUS_VERSION} ] || tar xf opus-${OPUS_VERSION}.tar.gz

# Optimization flags for ARM Cortex-A7 with NEON
OPTIM_CFLAGS="-O3 -mcpu=cortex-a7 -mfpu=neon -mfloat-abi=hard -ftree-vectorize -ffast-math -funroll-loops"

export CC="${CROSS_PREFIX}-gcc"
export CFLAGS="$OPTIM_CFLAGS"
export CXXFLAGS="$OPTIM_CFLAGS"

# Build ALSA
cd alsa-lib-${ALSA_VERSION}
if [ ! -f .built ]; then
  CFLAGS="$OPTIM_CFLAGS" ./configure --host arm-rockchip830-linux-uclibcgnueabihf --enable-static=yes --enable-shared=no --with-pcm-plugins=rate,linear --disable-seq --disable-rawmidi --disable-ucm
  make -j$(nproc)
  touch .built
fi
cd ..

# Build Opus
cd opus-${OPUS_VERSION}
if [ ! -f .built ]; then
  CFLAGS="$OPTIM_CFLAGS" ./configure --host arm-rockchip830-linux-uclibcgnueabihf --enable-static=yes --enable-shared=no --enable-fixed-point
  make -j$(nproc)
  touch .built
fi
cd ..

echo "ALSA and Opus built in $AUDIO_LIBS_DIR"
