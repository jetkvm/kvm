#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE="${1:-debug}"
OUT="$ROOT/build"
GEN="$OUT/gen"
CLASSES="$OUT/classes"
DEX="$OUT/dex"
RES_ZIP="$OUT/resources.zip"
UNSIGNED="$OUT/JetKVM-Companion-unsigned.apk"
ALIGNED="$OUT/JetKVM-Companion-aligned.apk"

case "$MODE" in
  debug)
    SIGNED="$OUT/JetKVM-Companion-debug.apk"
    KEYSTORE="${KEYSTORE:-${JETKVM_COMPANION_DEBUG_KEYSTORE:-$HOME/.local/share/jetkvm-companion/jetkvm-companion-debug.keystore}}"
    KEY_ALIAS="${KEY_ALIAS:-jetkvm-companion}"
    KEY_DNAME="${KEY_DNAME:-CN=JetKVM Companion Debug,O=JetKVM}"
    ;;
  release)
    SIGNED="$OUT/JetKVM-Companion-release.apk"
    KEYSTORE="${KEYSTORE:-$ROOT/jetkvm-companion-release.keystore}"
    KEY_ALIAS="${KEY_ALIAS:-jetkvm-companion-release}"
    KEY_DNAME="${KEY_DNAME:-CN=JetKVM Companion Release,O=JetKVM}"
    ;;
  *)
    echo "Usage: $0 [debug|release]"
    exit 1
    ;;
esac

STOREPASS="${STOREPASS:-android}"
KEYPASS="${KEYPASS:-$STOREPASS}"

ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
BUILD_TOOLS="${BUILD_TOOLS:-$ANDROID_HOME/build-tools/36.1.0}"
PLATFORM="${PLATFORM:-$ANDROID_HOME/platforms/android-36/android.jar}"

AAPT2="$BUILD_TOOLS/aapt2"
APKSIGNER="$BUILD_TOOLS/apksigner"
D8="$BUILD_TOOLS/d8"
ZIPALIGN="$BUILD_TOOLS/zipalign"

for tool in "$AAPT2" "$APKSIGNER" "$D8" "$ZIPALIGN" javac keytool; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "ERROR: missing tool: $tool"
    exit 1
  }
done

[ -f "$PLATFORM" ] || {
  echo "ERROR: missing Android platform jar: $PLATFORM"
  exit 1
}

rm -rf "$OUT"
mkdir -p "$GEN" "$CLASSES" "$DEX"

"$AAPT2" compile --dir "$ROOT/res" -o "$RES_ZIP"
"$AAPT2" link \
  -I "$PLATFORM" \
  --manifest "$ROOT/AndroidManifest.xml" \
  --java "$GEN" \
  -o "$UNSIGNED" \
  "$RES_ZIP"

javac -source 8 -target 8 \
  -bootclasspath "$PLATFORM" \
  -classpath "$GEN" \
  -d "$CLASSES" \
  $(find "$ROOT/src" "$GEN" -name '*.java' | sort)

"$D8" --lib "$PLATFORM" --output "$DEX" $(find "$CLASSES" -name '*.class' | sort)
(cd "$DEX" && zip -qr "$UNSIGNED" classes.dex)

"$ZIPALIGN" -f 4 "$UNSIGNED" "$ALIGNED"

mkdir -p "$(dirname "$KEYSTORE")"
if [ ! -f "$KEYSTORE" ]; then
  keytool -genkeypair \
    -keystore "$KEYSTORE" \
    -storepass "$STOREPASS" \
    -keypass "$KEYPASS" \
    -alias "$KEY_ALIAS" \
    -keyalg RSA \
    -keysize 2048 \
    -validity 10000 \
    -dname "$KEY_DNAME"
fi

"$APKSIGNER" sign \
  --ks "$KEYSTORE" \
  --ks-pass "pass:$STOREPASS" \
  --key-pass "pass:$KEYPASS" \
  --out "$SIGNED" \
  "$ALIGNED"

"$APKSIGNER" verify "$SIGNED"

echo "Built: $SIGNED"
