JetKVM
<div align="center"> <img alt="JetKVM logo" src="https://jetkvm.com/logo-blue.png" height="28"> </div>

Links
Discord | Website | Issues | Docs

About JetKVM
JetKVM is a high-performance, open-source KVM-over-IP (Keyboard, Video, Mouse) solution designed for efficient remote management of computers, servers, and workstations. Whether you're troubleshooting boot failures, installing a new operating system, adjusting BIOS settings, or simply taking control of a machine remotely, JetKVM provides the tools to get the job done.

Features
Ultra-low Latency – 1080p@60FPS video with 30-60ms latency using H.264 encoding, ensuring smooth mouse and keyboard interaction.

Free & Optional Remote Access – Manage your device remotely via JetKVM Cloud using WebRTC.

Open-source Software – Written in Golang for Linux, allowing easy customization through SSH access.

Contributing
We welcome contributions from the community! Whether it's improving the firmware, adding new features, or enhancing documentation, your input is valuable. Please review our Code of Conduct before contributing.

Getting Help
For answers to common questions, check out our Documentation. If you need additional support, join our Discord Server.

Reporting Issues
If you've found a bug, please report it via our Issues page. Be sure to include your firmware version, platform details, and clear steps to reproduce the issue.

Development
JetKVM is written in Go and TypeScript, with some components in C. An intermediate level of Go and TypeScript knowledge is recommended for contributing.

The project consists of two main parts:

Backend Software – Runs on the KVM device, manages device functionality and cloud API.

Frontend Software – Built with React and TypeScript, served by the KVM device and cloud.

Development Setup
For local development, use the ./dev_deploy.sh script to build and deploy the frontend and backend to your KVM device. Run ./dev_deploy.sh --help for more details.
