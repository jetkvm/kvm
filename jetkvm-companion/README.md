# JetKVM Companion APK

Small target-side Android helper for JetKVM Android target setups.

JetKVM controls Android targets through USB HID touchscreen and keyboard input.
On stock Android, that is enough once the phone is usable, but the lockscreen is
special: Android can keep a trusted soft keyguard on the external JetKVM display
and refuse to dismiss it from the external USB digitizer. This companion uses
only public Android APIs to bridge that gap.

The companion exists because this is an Android multi-display keyguard policy
boundary, not a JetKVM HID bug. AOSP documents that secondary-display lockscreen
UI doesn't support unlocking from secondary screens, and its multi-display FAQ
says the default secondary-display lockscreen isn't interactive and doesn't
allow unlocking.

References:

- https://source.android.com/docs/core/display/multi_display/lock-screen
- https://source.android.com/docs/core/display/multi_display/faq

The companion does not inject input, capture the screen, use ADB, require root,
use Accessibility, or depend on Shizuku. It runs a foreground service, listens
for display off/on events, and keeps a transparent `showWhenLocked` Activity
available. When the target wakes and Android reports the user is trusted, the
Activity calls `KeyguardManager.requestDismissKeyguard()`.

Opening the app shows a small settings UI. Use **Arm companion** after install,
grant **Background launch assist**, and enable **Launch on boot** if the helper
should arm itself after Android finishes booting. Android 13 and later may ask
for notification permission; that permission lets Android keep the companion
foreground service visible and reliable.

Automatic wake-unlock from the background requires Android's overlay permission.
The companion uses it for a tiny non-touchable launch-assist overlay. This gives
Android a visible non-app window for the foreground service, which allows the
transparent dismiss Activity to launch on display off/on without leaving an
interactive overlay on top of the target phone.

## Modes of Operation

- **No lockscreen**: may work for some users, but some apps are hostile toward
  disabled lockscreen or insecure-device configurations.
- **Keyguard on with Extend Unlock**: recommended stock-Android mode for this
  helper. Keep the normal Android keyguard enabled, configure Extend Unlock or
  another trusted state, install JetKVM Companion on the target phone, and open
  it once after boot to arm the foreground service. Grant **Background launch
  assist** for automatic wake-unlock while the app is in the background. Enable
  **Launch on boot** to arm the service automatically after future boots.
- **Keyguard on without Extend Unlock**: no stock/public JetKVM solution. A hard
  locked Android device requires the user credential or third-party automation
  tools such as Tasker, Shizuku-based automation, Accessibility automation, root,
  or device-owner/OEM privileges.

## Build

```bash
cd /path/to/kvm
./jetkvm-companion/build.sh
```

## Release Build

```bash
cd /path/to/kvm
./jetkvm-companion/build.sh release
```

## Install

```bash
adb install -r jetkvm-companion/build/JetKVM-Companion-debug.apk
```

Install the release APK:

```bash
adb install -r jetkvm-companion/build/JetKVM-Companion-release.apk
```

Latest release:

```text
https://github.com/Batestinha/jetkvm-companion/releases/tag/v1.1
```

Latest APK asset:

```text
JetKVM-Companion-1.1.apk
```

Obtainium source:

```text
https://github.com/Batestinha/jetkvm-companion
```

This release-only repository is separate from the main JetKVM fork so Obtainium
can track the target companion APK independently from the controller APK.
