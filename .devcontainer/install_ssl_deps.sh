#!/bin/bash
# .devcontainer/install_ssl_deps.sh
# Build OpenSSL static libs for ARM with cryptodev hardware acceleration support
# Output is fully statically linked - no runtime dependencies on the device
set -e

# Sudo wrapper function
function use_sudo() {
  if [ "$UID" -eq 0 ] || [ -z "$(which sudo 2>/dev/null)" ]; then
    "$@"
  else
    sudo -E "$@"
  fi
}

# Accept version parameter or use default
OPENSSL_VERSION="${1:-3.6.0}"

SSL_LIBS_DIR="/opt/jetkvm-ssl-libs"
BUILDKIT_PATH="/opt/jetkvm-native-buildkit"
BUILDKIT_FLAVOR="arm-rockchip830-linux-uclibcgnueabihf"
CROSS_PREFIX="$BUILDKIT_PATH/bin/$BUILDKIT_FLAVOR"
SYSROOT="$BUILDKIT_PATH/$BUILDKIT_FLAVOR/sysroot"

# Install cryptodev.h header for /dev/crypto hardware acceleration
# This header is required for OpenSSL's devcrypto engine to build
# The header is included in our source tree (from Luckfox Pico kernel, same RV1106 SoC)
CRYPTO_HEADER_DIR="$SYSROOT/usr/include/crypto"
CRYPTO_HEADER="$CRYPTO_HEADER_DIR/cryptodev.h"
CRYPTO_HEADER_SRC="$(dirname "$0")/../internal/hal/crypto/tls/include/crypto/cryptodev.h"

if [ ! -f "$CRYPTO_HEADER" ]; then
  echo "Installing cryptodev.h header for hardware crypto support..."
  use_sudo mkdir -p "$CRYPTO_HEADER_DIR"
  use_sudo cp "$CRYPTO_HEADER_SRC" "$CRYPTO_HEADER"
  echo "Installed: $CRYPTO_HEADER (from source tree)"
fi

# Create directory with proper permissions
use_sudo mkdir -p "$SSL_LIBS_DIR"
use_sudo chmod 777 "$SSL_LIBS_DIR"
cd "$SSL_LIBS_DIR"

# Download source
[ -f openssl-${OPENSSL_VERSION}.tar.gz ] || wget -N https://github.com/openssl/openssl/releases/download/openssl-${OPENSSL_VERSION}/openssl-${OPENSSL_VERSION}.tar.gz

# Extract
[ -d openssl-${OPENSSL_VERSION} ] || tar xf openssl-${OPENSSL_VERSION}.tar.gz

# ARM Cortex-A7 optimization flags with position-independent code for static linking
OPTIM_CFLAGS="-O2 -mfpu=neon -mtune=cortex-a7 -mfloat-abi=hard -fPIC -DNDEBUG"

# Build OpenSSL
cd openssl-${OPENSSL_VERSION}
if [ ! -f .built ]; then
  chown -R $(whoami):$(whoami) .

  # Configure OpenSSL for ARM cross-compile with static libs only
  # - no-shared: Build only static libraries (.a files)
  # - enable-devcryptoeng: Enable /dev/crypto hardware acceleration (RV1106 Rockchip crypto)
  # - enable-ktls: Enable kernel TLS (kTLS) for zero-copy encryption via sendmsg()
  #   When kernel has CONFIG_TLS=y, OpenSSL will offload TLS encryption to kernel,
  #   which uses hardware crypto via CONFIG_CRYPTO_DEV_ROCKCHIP
  # - enable-weak-ssl-ciphers: Enable ADH (anonymous DH) ciphers for VNC TLS compatibility
  # - threads: Enable thread safety
  # Note: We keep engines enabled for devcrypto hardware acceleration
  ./Configure linux-armv4 \
    --cross-compile-prefix=${CROSS_PREFIX}- \
    --prefix="$SSL_LIBS_DIR/install" \
    --openssldir="$SSL_LIBS_DIR/install/ssl" \
    no-shared \
    enable-devcryptoeng \
    enable-ktls \
    enable-weak-ssl-ciphers \
    threads \
    $OPTIM_CFLAGS

  # Build only libraries (not apps)
  make -j$(nproc) build_libs

  # Install headers and static libs
  make install_dev

  touch .built
fi
cd ..

# Verify static libraries were built
echo ""
echo "Verifying static libraries:"
ls -la "$SSL_LIBS_DIR/install/lib64/"*.a 2>/dev/null || ls -la "$SSL_LIBS_DIR/install/lib/"*.a

echo ""
echo "OpenSSL ${OPENSSL_VERSION} built in $SSL_LIBS_DIR/install (static only)"
echo ""
echo "Hardware crypto support:"
if [ -f "$CRYPTO_HEADER" ]; then
  echo "  ✓ cryptodev.h header installed"
  echo "  ✓ devcrypto engine enabled (uses /dev/crypto on RV1106)"
else
  echo "  ✗ cryptodev.h header missing - hardware crypto NOT available"
fi
echo ""
echo "The Makefile will automatically use this OpenSSL when detected."
