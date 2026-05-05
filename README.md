<div align="center">
    <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28">

### KVM

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)

[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)

[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>

> [!NOTE]
> This fork contains in-progress Android support work for
> [PR #1441](https://github.com/jetkvm/kvm/pull/1441): Android touchscreen HID
> target support, Android/mobile compact controller UI, and a native Android
> controller APK.
>
> Android controller APK release:
> [JetKVM Android Support 1.4](https://github.com/Batestinha/kvm/releases/tag/jetkvm-android-controller-v1.4)

## Android Support Fork

This fork is an Android-focused JetKVM experiment and upstream PR. It keeps the
normal JetKVM device, web UI, and firmware structure, but adds support for two
Android-specific workflows:

- **Android as a target device**: control a stock Android phone through JetKVM
  using USB HID touchscreen input instead of absolute mouse input.
- **Android as a controller device**: use another Android phone or tablet as the
  controller for the JetKVM web UI through a compact mobile UI or native wrapper
  APK.

The target use case is a stock Android phone connected to JetKVM as the remote
device. Video still comes from the JetKVM capture path. Input is sent through a
USB HID digitizer so Android sees direct touch events, not mouse events. This
avoids Android cursor behavior and makes taps, drags, and scroll behavior match
what Android apps expect from a real touchscreen.

### Android Target Support

The backend adds a USB HID touchscreen/digitizer gadget and a `touchscreenReport`
RPC path. When Android touchscreen mode is active, the browser maps pointer
events over the video to HID touchscreen coordinates and sends:

- one-contact touch down/move/up reports,
- direct `ABS_X`/`ABS_Y` style coordinates in the HID range,
- wheel events as HID wheel reports rather than fake swipe gestures.

This is intended to address Android target input problems where Android treats
JetKVM absolute mouse input as a mouse, producing cursor/IME behavior instead of
normal touch behavior.

### Android Controller UI

The web UI includes an Android compact controller mode. In that mode it removes
desktop-oriented chrome around the stream and replaces the usual header/action
bars with a draggable floating control button. The floating menu exposes the
same core JetKVM actions while preserving more screen space for the remote phone
video.

The compact mode also scopes phone-shaped display crop behavior to Android
compact controller mode only, so desktop users keep the normal JetKVM video
layout.

### Native Android Controller APK

The `jetkvm-android/` directory contains a small native Android wrapper. It
opens a native login screen first so Android password managers can autofill the
JetKVM password reliably, then loads the JetKVM web UI in a fullscreen WebView.

The wrapper accepts HTTPS URLs and local HTTP JetKVM URLs. Local HTTP is limited
in the native login flow to localhost, `.local` hostnames, IPv4 private ranges,
and link-local IPv4 addresses. This keeps raw JetKVM LAN IPs usable while
rejecting arbitrary public cleartext HTTP URLs before the WebView is opened.

Latest APK:

- Release:
  [JetKVM Android Support 1.4](https://github.com/Batestinha/kvm/releases/tag/jetkvm-android-controller-v1.4)
- Asset:
  `JetKVM-android-controller-1.4.apk`

### Upstream Status

This work is proposed upstream in
[jetkvm/kvm PR #1441](https://github.com/jetkvm/kvm/pull/1441). The PR is still
draft while review feedback is being handled. The fork keeps the implementation
visible and testable while that upstream discussion continues.

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
