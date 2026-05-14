# JetKVM Android Controller

Native Android wrapper for the JetKVM Android controller.

The controller APK opens JetKVM in Android controller mode and owns the
Android-specific pieces that do not belong in the desktop browser UI:

- Native login activity with Android Autofill support.
- Immersive phone controller view.
- Compact floating controls overlay.
- Android OSK bridge for text input.
- Display controls and logout from the compact overlay.
- Desktop virtual-keyboard fallback preserved for non-APK browser sessions.

The app dispatches the Android controller URL internally:

```text
http://jetkvm.local/?jetkvmAndroid=1
```

The login screen asks for the JetKVM host/IP and password; users do not need to
type URL parameters manually.

## Build

```bash
cd /path/to/kvm
./jetkvm-android/build.sh
```

## Release Build

```bash
cd /path/to/kvm
./jetkvm-android/build.sh release
```

## Install

```bash
adb install -r jetkvm-android/build/JetKVM-debug.apk
```

Install the release APK:

```bash
adb install -r jetkvm-android/build/JetKVM-release.apk
```

Latest release:

```text
https://github.com/Batestinha/jetkvm-android-controller/releases/tag/v1.11
```

Latest APK asset:

```text
JetKVM-Android-Controller-1.11.apk
```

Obtainium source:

```text
https://github.com/Batestinha/jetkvm-android-controller
```

This release-only repository is separate from the main JetKVM fork so Obtainium
can track the controller APK independently from the target companion APK.
