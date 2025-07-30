#!/bin/bash
# tools/build_audio_deps.sh
# Build ALSA and Opus static libs for ARM in $HOME/.jetkvm/audio-libs
set -e
JETKVM_HOME="$HOME/.jetkvm"
AUDIO_LIBS_DIR="$JETKVM_HOME/audio-libs"
TOOLCHAIN_DIR="$JETKVM_HOME/rv1106-system"
CROSS_PREFIX="$TOOLCHAIN_DIR/tools/linux/toolchain/arm-rockchip830-linux-uclibcgnueabihf/bin/arm-rockchip830-linux-uclibcgnueabihf"

mkdir -p "$AUDIO_LIBS_DIR"
cd "$AUDIO_LIBS_DIR"

# Download sources
[ -f alsa-lib-1.2.14.tar.bz2 ] || wget -N https://www.alsa-project.org/files/pub/lib/alsa-lib-1.2.14.tar.bz2
[ -f opus-1.5.2.tar.gz ] || wget -N https://downloads.xiph.org/releases/opus/opus-1.5.2.tar.gz

# Extract
[ -d alsa-lib-1.2.14 ] || tar xf alsa-lib-1.2.14.tar.bz2
[ -d opus-1.5.2 ] || tar xf opus-1.5.2.tar.gz

export CC="${CROSS_PREFIX}-gcc"

# Build ALSA
cd alsa-lib-1.2.14
if [ ! -f .built ]; then
  ./configure --host arm-rockchip830-linux-uclibcgnueabihf --enable-static=yes --enable-shared=no --with-pcm-plugins=rate,linear --disable-seq --disable-rawmidi --disable-ucm
  make -j$(nproc)
  touch .built
fi
cd ..

# Build Opus
cd opus-1.5.2
if [ ! -f .built ]; then
  ./configure --host arm-rockchip830-linux-uclibcgnueabihf --enable-static=yes --enable-shared=no --enable-fixed-point
  make -j$(nproc)
  touch .built
fi
cd ..

echo "ALSA and Opus built in $AUDIO_LIBS_DIR"
