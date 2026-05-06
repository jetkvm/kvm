# JetKVM Android Controller APK

Native Android wrapper for using a phone or tablet as a JetKVM controller. This
is the controller-side companion to the Android support work in this fork; the
target-side input changes live in the main JetKVM backend and web UI, and the
optional target-side keyguard helper lives in `../jetkvm-companion/`.

The app shows a native login screen first so Android password managers can
reliably autofill the JetKVM password. After login, it opens the JetKVM web UI
in Android controller mode inside a fullscreen WebView:

```text
http://jetkvm.local/?jetkvmAndroid=1
```

Android controller mode keeps the stream as the primary surface, hides the
desktop-oriented header/action bars, and exposes those actions through a
draggable floating control button. The button is clamped to the viewport so it
remains reachable.

The native login screen stores the last controller URL. Use the in-app logout
action to return to the native login screen and change the URL or credentials.
The **Stay logged in** checkbox controls whether the JetKVM session persists
across app/browser restarts; the checkbox state is remembered.

The wrapper accepts HTTPS URLs and local HTTP JetKVM URLs. Local HTTP is limited
in the native login flow to localhost, `.local` hostnames, IPv4 private ranges,
and link-local IPv4 addresses. This keeps raw JetKVM LAN IPs usable while
rejecting arbitrary public cleartext HTTP URLs before the WebView is opened.

Latest release:

```text
https://github.com/Batestinha/jetkvm-android-controller/releases/tag/v1.5
```

Latest APK asset:

```text
JetKVM-android-controller-1.5.apk
```

Obtainium source:

```text
https://github.com/Batestinha/jetkvm-android-controller
```

This release-only repository is separate from the main JetKVM fork so Obtainium
can track the controller APK independently from the target companion APK.

Build:

```bash
cd /path/to/kvm
./jetkvm-android/build.sh
```

Release build:

```bash
cd /path/to/kvm
./jetkvm-android/build.sh release
```

Install:

```bash
adb install -r jetkvm-android/build/JetKVM-debug.apk
```

Install the release APK:

```bash
adb install -r jetkvm-android/build/JetKVM-release.apk
```
