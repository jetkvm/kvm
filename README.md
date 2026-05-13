# JetKVM Android Support Fork

> [!NOTE]
> This Android support fork is work in progress. The current branch is usable
> for local testing, but it is still being refined through real device testing.
> Bug reports, focused review, and help with Android target/controller edge
> cases are welcome.

This fork adds an Android-focused control path to JetKVM. It is built for the
case where the JetKVM target is an Android device and the operator wants the
same practical control surface that a physical touchscreen, keyboard, and mouse
would provide.

The current branch is based on a validated known-good touchscreen and aspect
baseline. The important baseline properties are:

- Android digitizer input is routed through USB HID touchscreen emulation.
- Touch coordinates are aligned with the captured video and feel smooth in use.
- The Android target aspect ratio is preserved for the phone controller view.
- Desktop JetKVM usability remains available for non-Android workflows.

## What This Fork Adds

- **Android USB touchscreen target support** - JetKVM can expose a direct-touch
  HID digitizer path for Android targets instead of treating touch as generic
  mouse input.
- **Android controller APK** - A native Android wrapper for operating JetKVM
  from a phone. It supplies the Android-specific login flow, immersive view,
  native OSK integration, and compact controller defaults.
- **Compact controller mode** - The phone controller gets a reduced UI without
  the desktop chrome and button strip. Android-only controls are placed in a
  draggable floating menu.
- **Floating control overlay** - The overlay includes target actions such as
  paste text, virtual media, Wake-on-LAN, virtual keyboard, display toggle,
  logout, settings, and connection tools while preserving the video surface for
  touch input.
- **Native Android login activity** - The controller APK owns the login
  experience instead of relying on the vanilla web auth page. This keeps
  password managers and Autofill useful on the controller phone.
- **Android OSK bridge** - In the controller APK, the compact virtual keyboard
  action opens Android's own input method and forwards committed text through
  JetKVM's existing HID keyboard macro path. Desktop browsers still use the
  regular web virtual keyboard.
- **Display toggle for Android targets** - The UI can send a harmless HID key
  event to wake the display, or the Android display power shortcut when
  available.
- **Relative HID wheel scrolling** - Mouse wheel input is routed through the
  relative HID mouse path when Android exposes one, preserving touchscreen
  alignment while restoring useful wheel behavior.
- **Android target companion APK** - A small target-side helper runs on the
  Android phone being controlled. It handles the Android keyguard edge cases
  that USB HID input alone cannot solve, without using root, ADB, Accessibility,
  Shizuku, or screen capture.

## How It Works

The backend keeps JetKVM's normal video, WebRTC, keyboard, virtual media, and
device-management paths. Android-specific input is layered on top where it is
needed:

1. The JetKVM device exposes HID endpoints suitable for an Android target.
2. Touchscreen events from the viewer are mapped to the captured Android frame
   and sent through the absolute HID digitizer path.
3. Wheel events use the relative mouse HID path when present, because Android
   handles wheel scrolling differently from direct touchscreen gestures.
4. The Android controller APK identifies itself to the backend by opening the
   controller URL with Android compact-mode parameters.
5. The controller APK replaces the web auth page with a native login activity
   so Autofill and Android keyboard behavior work naturally.
6. The compact overlay keeps Android-only actions close to the controller view
   without polluting the desktop JetKVM interface.
7. The companion APK pairs with trusted JetKVM devices, watches Android's own
   view of the paired JetKVM USB/display identity, and grants the backend a
   short Android target lease only while that physical evidence is present.

## Why This Exists

Vanilla JetKVM is designed as a general KVM over IP. Android targets are
different enough that the generic desktop assumptions are not enough:

- Android distinguishes direct touchscreen input from mouse input.
- A phone-shaped captured display needs strict aspect handling or touches drift.
- Android lockscreen behavior has policy boundaries that generic HID input
  cannot always cross cleanly.
- A phone controller needs a different UI density from the desktop browser UI.
- Android users expect Autofill, the native OSK, and immersive full-screen app
  behavior instead of a desktop-style login form.

This fork keeps those Android-specific decisions explicit. The goal is not to
replace JetKVM's normal UI; it is to add a focused Android target/controller
path while leaving the vanilla experience recognizable.

## Target Companion

The companion APK is installed on the Android target, not on the controller
phone. Its job is narrow: make JetKVM-controlled Android targets recover
cleanly from display wake and soft keyguard states while keeping the trust and
presence boundary on the target device.

JetKVM can send touchscreen, keyboard, mouse, wheel, and display-toggle input
over USB HID. That is enough once Android is interactive. The lockscreen is the
exception. On stock Android, secondary-display keyguard behavior is governed by
Android multi-display policy, and the external display shown through JetKVM may
not be allowed to dismiss a trusted keyguard purely from the external USB
digitizer. That is an Android policy boundary, not a broken touch coordinate
path.

The companion uses public Android APIs and an authenticated pairing flow to
bridge that boundary:

- Pairing establishes trust between the Android companion and one or more
  JetKVM endpoints. The backend rejects companion target declarations unless
  they authenticate with a paired companion token.
- Each JetKVM exposes a stable per-device identity through the USB gadget
  serial/product strings and through the default EDID monitor name/serial. On
  Android this appears in input devices such as `JetKVM USB Emulation Device
  <short id>` and in the external display name as `JKVM <short id>`.
- The companion stores the paired JetKVM endpoint, paired token, and expected
  JetKVM identity.
- Its foreground service reflects state: before pairing it waits for a device
  to be paired, after pairing it waits for matching peripherals, and once
  physical evidence is present it monitors display-on events.
- Presence is OR-based. Matching keyboard, digitizer/touchscreen, mouse/pointer,
  or monitor evidence is enough to activate the companion path.
- While matching evidence remains visible to Android, the companion refreshes an
  authenticated Android target lease to the paired JetKVM. The lease reports the
  JetKVM identity, Android target type, preferred digitizer mode, display
  dimensions/aspect, and the evidence list. Disconnection is reported
  immediately when evidence disappears; lease expiry is the backend fallback for
  companion crashes or network loss.
- When the target wakes, it launches a transparent `showWhenLocked` activity and
  calls `KeyguardManager.requestDismissKeyguard()`. Android decides whether the
  keyguard can be dismissed.
- Around display-on events, the companion briefly creates a transparent 1x1
  `Presentation` on the JetKVM external display to keep that display path awake
  without dimming or presenting UI.
- It can optionally use Android's overlay permission as a non-touchable launch
  assist so background wake-unlock remains reliable after the display turns on.

What it deliberately does not do:

- It does not inject input.
- It does not capture or read the screen.
- It does not use ADB.
- It does not require root, Shizuku, Accessibility, device-owner privileges, or
  OEM-only APIs.
- It does not accept generic USB or generic display metadata as proof. Evidence
  must match the paired JetKVM identity before Android mode is granted.

The recommended mode is normal Android keyguard with the companion foreground
service installed, notification permission granted where Android requires it,
unrestricted battery enabled for reliability, at least one JetKVM paired, and
launch-on-boot enabled if the target should recover after reboots. A trusted
state such as Extend Unlock can make wake recovery automatic. Without a trusted
state, the companion can still bring up Android's credential bouncer after wake;
the user can then enter the PIN or password through JetKVM keyboard input or the
Android controller's OSK bridge.

## Current Validation State

The current support branch has been locally validated with:

- Android digitizer touch input.
- Correct phone-controller aspect and crop behavior.
- Desktop viewer behavior preserved.
- Compact overlay actions.
- Display wake/toggle actions.
- Native Android login and logout flow.
- Autofill password entry with OSK collapse handling.
- Companion app permission UI cleanup.
- Relative HID mouse wheel scrolling.
- Controller APK Android OSK text forwarding.
- Authenticated companion pairing and Android lease renewal.
- JetKVM identity binding through USB gadget strings and EDID display identity.
- Companion activation from matching keyboard, digitizer, mouse, or monitor
  evidence.

Non-trivial changes to this fork should be built, deployed, and tested on the
JetKVM device plus the controller/target phones before being committed or
published.

## Components

- `jetkvm-android/` - Android controller APK.
- `jetkvm-companion/` - Android target companion APK.
- `ui/src/components/AndroidCompactControls.tsx` - compact Android controller
  overlay.
- Backend HID/RPC changes live in the normal JetKVM backend tree.

## Build Notes

Build the JetKVM backend and device UI:

```bash
make build_dev
```

Build the Android controller APK:

```bash
./jetkvm-android/build.sh release
```

Build the Android companion APK:

```bash
./jetkvm-companion/build.sh release
```

Install APKs with ADB as usual:

```bash
adb install -r jetkvm-android/build/JetKVM-release.apk
adb install -r jetkvm-companion/build/JetKVM-Companion-release.apk
```

## Upstream README

The section below is the vanilla upstream JetKVM README, kept intact for
project context.

---

<div align="center">
    <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28">

### KVM

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)

[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)

[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>

JetKVM is a high-performance, open-source KVM over IP (Keyboard, Video, Mouse) solution designed for efficient remote management of computers, servers, and workstations. Whether you're dealing with boot failures, installing a new operating system, adjusting BIOS settings, or simply taking control of a machine from afar, JetKVM provides the tools to get it done effectively.

## Features

- **Ultra-low Latency** - 1080p@60FPS video with 30-60ms latency using H.264 encoding. Smooth mouse and keyboard interaction for responsive remote control.
- **Free & Optional Remote Access** - Remote management via JetKVM Cloud using WebRTC.
- **Optional Tailscale Networking** - Built-in Tailscale status and control-server configuration, including custom [Headscale](https://headscale.net/)-compatible endpoints.
- **Open-source software** - Written in Golang on Linux. Easily customizable through SSH access to the JetKVM device.

## Contributing

We welcome contributions from the community! Whether it's improving the firmware, adding new features, or enhancing documentation, your input is valuable. We also have some rules and taboos here, so please read this page and our [Code of Conduct](/CODE_OF_CONDUCT.md) carefully.

## I need help

The best place to search for answers is our [Documentation](https://jetkvm.com/docs). If you can't find the answer there, check our [Discord Server](https://jetkvm.com/discord).

## I want to report an issue

If you've found an issue and want to report it, please check our [Issues](https://github.com/jetkvm/kvm/issues) page. Make sure the description contains information about the firmware version you're using, your platform, and a clear explanation of the steps to reproduce the issue.

# Development

JetKVM is written in Go & TypeScript. with some bits and pieces written in C. An intermediate level of Go & TypeScript knowledge is recommended for comfortable programming.

The project contains two main parts, the backend software that runs on the KVM device and the frontend software that is served by the KVM device, and also the cloud.

For comprehensive development information, including setup, testing, debugging, and contribution guidelines, see **[DEVELOPMENT.md](DEVELOPMENT.md)**.

For quick device development, use the `./dev_deploy.sh` script. It will build the frontend and backend and deploy them to the local KVM device. Run `./dev_deploy.sh --help` for more information.

## Backend

The backend is written in Go and is responsible for the KVM device management, the cloud API and the cloud web.

## Frontend

The frontend is written in React and TypeScript and is served by the KVM device. It has three build targets: `device`, `development` and `production`. Development is used for development of the cloud version on your local machine, device is used for building the frontend for the KVM device and production is used for building the frontend for the cloud.
