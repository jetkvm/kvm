<div align="center">
    <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28">

### KVM

[Discord](https://jetkvm.com/discord) | [Website](https://jetkvm.com) | [Issues](https://github.com/jetkvm/cloud-api/issues) | [Docs](https://jetkvm.com/docs)

[![Twitter](https://img.shields.io/twitter/url/https/twitter.com/jetkvm.svg?style=social&label=Follow%20%40JetKVM)](https://twitter.com/jetkvm)

[![Go Report Card](https://goreportcard.com/badge/github.com/jetkvm/kvm)](https://goreportcard.com/report/github.com/jetkvm/kvm)

</div>



JetKVM is a high-performance, open-source KVM over IP (Keyboard, Video, Mouse, **Audio**) solution designed for efficient remote management of computers, servers, and workstations. Whether you're dealing with boot failures, installing a new operating system, adjusting BIOS settings, or simply taking control of a machine from afar, JetKVM provides the tools to get it done effectively.





## Features

- **Ultra-low Latency** - 1080p@60FPS video with 30-60ms latency using H.264 encoding. Smooth mouse, keyboard, and audio for responsive remote control.
- **First-Class Audio Support** - JetKVM now supports in-process, low-latency audio streaming using ALSA and Opus, fully integrated via CGO. No external audio binaries or IPC required—audio is delivered directly from the device to your browser.
- **Free & Optional Remote Access** - Remote management via JetKVM Cloud using WebRTC.
- **Open-source software** - Written in Golang (with CGO for audio) on Linux. Easily customizable through SSH access to the JetKVM device.

## Contributing

We welcome contributions from the community! Whether it's improving the firmware, adding new features, or enhancing documentation, your input is valuable. We also have some rules and taboos here, so please read this page and our [Code of Conduct](/CODE_OF_CONDUCT.md) carefully.

## I need help

The best place to search for answers is our [Documentation](https://jetkvm.com/docs). If you can't find the answer there, check our [Discord Server](https://jetkvm.com/discord).

## I want to report an issue

If you've found an issue and want to report it, please check our [Issues](https://github.com/jetkvm/kvm/issues) page. Make sure the description contains information about the firmware version you're using, your platform, and a clear explanation of the steps to reproduce the issue.



# Development

JetKVM is written in Go & TypeScript, with some C for low-level integration. **Audio support is now fully in-process using CGO, ALSA, and Opus—no external audio binaries required.**

The project contains two main parts: the backend software (Go, CGO) that runs on the KVM device, and the frontend software (React/TypeScript) that is served by the KVM device and the cloud.

For comprehensive development information, including setup, testing, debugging, and contribution guidelines, see **[DEVELOPMENT.md](DEVELOPMENT.md)**.

For quick device development, use the `./dev_deploy.sh` script. It will build the frontend and backend and deploy them to the local KVM device. Run `./dev_deploy.sh --help` for more information.


## Backend

The backend is written in Go and is responsible for KVM device management, audio/video streaming, the cloud API, and the cloud web. **Audio is now captured and encoded in-process using ALSA and Opus via CGO, with no external processes or IPC.**

## Frontend

The frontend is written in React and TypeScript and is served by the KVM device. It has three build targets: `device`, `development`, and `production`. Development is used for the cloud version on your local machine, device is used for building the frontend for the KVM device, and production is used for building the frontend for the cloud.
