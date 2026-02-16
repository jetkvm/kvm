# HAL Progress Tracker

Last updated: 2026-02-16

## Goals (Non-Negotiable)

- JetKVM app retains 100% feature parity + equal or better performance vs `feat/rdp-server` (merged into `main` at `6882610`).
- All native hardware-interaction code (CGO/C/C++) lives **only** in the HAL git submodule at `internal/hal` (root repo: `/Users/dtk07/Desktop/Scrap/jetkvm-contrib/hal`, GitHub: `KrakenKVM/hal`).
- HAL is in-process (no IPC for the fast path).
- Build/deploy and lint invocation remain unchanged:
  - Deploy: `env TERM=xterm-256color devpod ssh kvm-local --command 'export TERM=xterm && export VERSION=0.4.9 && ./dev_deploy.sh -r 192.168.100.165 --disable-docker --install 2>&1' | tee ./deploy.log`
  - Lint: `env TERM=xterm-256color devpod ssh kvm --command 'export TERM=xterm && export VERSION=0.5.1 && make lint-fix'`

## Milestones

- [x] Tooling prep for multi-platform native builds (devcontainer, Makefile, scripts) while keeping JetKVM flows identical.
  - [x] Devcontainer: `GOPRIVATE`/`GONOSUMDB` + git `insteadOf` (https -> ssh) configured.
  - [x] Devcontainer: submodule auto-init on attach (`git submodule update --init --recursive`).
  - [x] Makefile: multi-platform wiring from `feature/hal-cutover-jetkvm`, with JetKVM defaults kept working (CGO + native/audio enabled by default when `TARGET_PLATFORM=jetkvm`).
  - [x] Build tags: gated JetKVM native/audio CGO via `jetkvm_native_cgo` / `jetkvm_audio_cgo` tags (ported from `feature/hal-cutover-jetkvm`).
- [x] Add HAL as submodule at `internal/hal` (SSH URL) + ensure devpod/devcontainer initializes submodules.
- [x] Populate HAL repo with extracted native code (JetKVM backend first).
- [x] Remove legacy native subprocess mode (gRPC/proxy) and native-mode UI controls (in-process HAL only).
- [x] Refactor app repo to remove all CGO/hardware-native code outside `internal/hal`.
- [x] Refactor app repo to remove all direct platform device access (`/dev`, `/sys`) outside `internal/hal` (watchdog, backlight, serial UART, NBD, diagnostics).
- [ ] Verify:
  - [x] `make lint-fix` via devpod (`kvm`) works unchanged.
  - [ ] Deploy command via devpod (`kvm-local`) works unchanged and device is fully functional.
  - [x] Grep check: no `import "C"` / `#cgo` outside `internal/hal`.

## Current Branch / Repos

- App repo: `/Users/dtk07/Desktop/Scrap/jetkvm-contrib/kvm`
  - Branch: `feat/hal-submodule`
  - Baseline reference: `main` already contains `feat/rdp-server` merge (`6882610`).
  - HAL submodule: `internal/hal` @ `02237c8` (remote: `git@github.com:KrakenKVM/hal.git`)
- HAL repo: `/Users/dtk07/Desktop/Scrap/jetkvm-contrib/hal` (remote: `git@github.com:KrakenKVM/hal.git`)

## Notes / Decisions

- Multi-platform wiring is being aligned with `feature/hal-cutover-jetkvm` and the reference repo `/Users/dtk07/Desktop/Scrap/KrakenKVM/kvm` (including `dpl.sh`), but JetKVM defaults must remain fully working.
- Git access for private deps/submodules: enforce `GOPRIVATE` + git URL rewriting (`insteadOf`) to SSH inside devcontainer/devpod.

## Latest Verification Logs

- 2026-02-16: `make lint-fix` succeeded in devpod env `kvm` with no issues.
- 2026-02-16: `make build_dev` succeeded in devpod env `kvm` (linux/amd64 cross-build for JetKVM).
- 2026-02-16: `make build_dev_comet` succeeded in devpod env `kvm` (linux/amd64 cross-build for comet-pro arm64, pure-Go).
- 2026-02-16: `make build_dev_nanokvm` succeeded in devpod env `kvm` (linux/amd64 cross-build for nanokvm-pro arm64, pure-Go).
- 2026-02-16: `make test` succeeded in devpod env `kvm`.
- 2026-02-16: Deploy attempt failed early: device not reachable at `192.168.100.165` (network/device availability).
