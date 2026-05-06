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
for JetKVM-identifiable Android input devices, and only enables keyguard
dismissal while Android sees the JetKVM keyboard plus touchscreen or pointer.
When the target wakes and Android reports the user is trusted, a transparent
`showWhenLocked` Activity calls `KeyguardManager.requestDismissKeyguard()`.

Opening the app shows a small settings UI. Use **Arm companion** after install,
grant **Background launch assist**, and enable **Launch on boot** if the helper
should arm itself after Android finishes booting. Android 13 and later may ask
for notification permission; that permission lets Android keep the companion
foreground service visible and reliable.

Use **Grant unrestricted battery** if Android's battery policy would otherwise
stop the primary peripheral watchdog after boot or during idle.

Automatic wake-unlock from the background requires Android's overlay permission.
The companion uses it for a tiny non-touchable launch-assist overlay. This gives
Android a visible non-app window for the foreground service, which allows the
transparent dismiss Activity to launch after display wake without leaving an
interactive overlay on top of the target phone.

The companion intentionally does not use generic external-monitor presence as
its arming condition. It snapshots Android `InputDevice` metadata at startup and
after input-device add/remove/change events. The key condition is JetKVM-named
input devices, currently `JetKVM USB Emulation Device`, with keyboard and either
touchscreen or pointer sources present.

## Modes of Operation

- **No lockscreen**: may work for some users, but some apps are hostile toward
  disabled lockscreen or insecure-device configurations.
- **Keyguard on with Extend Unlock**: recommended stock-Android mode for this
  helper. Keep the normal Android keyguard enabled, configure Extend Unlock or
  another trusted state, install JetKVM Companion on the target phone, and open
  it once after boot to arm the foreground service. Grant **Background launch
  assist** for automatic wake-unlock while the app is in the background, and
  grant **Unrestricted battery** for watchdog reliability. Enable **Launch on
  boot** to arm the service automatically after future boots.
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
https://github.com/Batestinha/jetkvm-companion/releases/tag/v1.2
```

Latest APK asset:

```text
JetKVM-Companion-1.2.apk
```

Obtainium source:

```text
https://github.com/Batestinha/jetkvm-companion
```

This release-only repository is separate from the main JetKVM fork so Obtainium
can track the target companion APK independently from the controller APK.
