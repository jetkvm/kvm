# JetKVM Android Controller

Minimal Android WebView wrapper for the JetKVM Android controller.

The app opens JetKVM in Android controller mode:

```text
http://jetkvm.local/?jetkvmAndroid=1
```

Long-press the screen to change the controller URL.

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
