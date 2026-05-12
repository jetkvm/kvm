# WIP Notes

## 2026-05-12

- `jetkvm-android-support` is the local baseline branch for this fork.
- The local JetKVM default artifact at `/userdata/jetkvm/bin/jetkvm_app`
  should always be built from the latest `batestinha/jetkvm-android-support`
  commit, not from upstream stock JetKVM.
- Experimental/debug builds may be deployed separately, but should not replace
  the default app unless they have been promoted into `jetkvm-android-support`.
- Synced `batestinha/jetkvm-android-support` and
  `batestinha/android-support-issues-pr-20260507` to the same code state.
- Enabled GitHub Issues on `Batestinha/kvm`.
- Released JetKVM Companion `v1.6`.
  - Release: `https://github.com/Batestinha/jetkvm-companion/releases/tag/v1.6`
  - APK: `JetKVM-Companion-1.6.apk`
  - SHA-256:
    `de5287d2c13777cb931adc58becdfdb75845dcc5d9ac366c6f895000fae54bb1`
- Built and installed the JetKVM default app from
  `jetkvm-android-support`.
  - Source commit before this note file:
    `09d090047d95950a7048027a08a48285a38ae1ff`
  - Built version: `0.5.8-dev202605120201`
  - Installed binary SHA-256:
    `e9dc54e2c5cbb9bbb6392b6500786e6751f3447c8efd783da71b6af684083c06`
  - Previous default was backed up on the JetKVM as:
    `/userdata/jetkvm/bin/jetkvm_app.pre-android-support-09d0900-20260512-020054`
- Verified the running JetKVM default app was the Android-support build and
  that the companion `/companion/target` endpoint responded successfully from
  the Pixel 8.
- Current upstream PR:
  `https://github.com/jetkvm/kvm/pull/1450`
  - Head branch: `android-support-issues-pr-20260507`
  - Old Bugbot findings from `915ddeb` are resolved:
    touchscreen mode default, display crop default, and touchscreen HID mutex.
  - Bugbot then reported four current findings against `09d0900`:
    unauthenticated companion target endpoint, digitizer pointer-capture
    coordinate handling, controller wake lock timeout, and Android aspect
    container width constraint.
  - After the README-only artifact-policy commit, GitHub CI had Go/UI lint
    passing while build and Bugbot were still in progress when work stopped.

## 2026-05-12 Follow-Up

- The local JetKVM default app was found running the stock/upstream binary
  again. Evidence:
  - `/userdata/jetkvm/bin/jetkvm_app` had SHA-256
    `ae9616fa9cf5877e2b5ca9d78b628308c990d7952354b72af284e9d74a39e1d3`.
  - That hash matched prior known wrong/default stock backups.
  - The companion `/companion/target` endpoint returned `404`.
- Restored the Android-support default app from the on-device artifact:
  `/userdata/jetkvm/bin/jetkvm_app.android-support-09d0900`.
- Added `scripts/local_jetkvm_baseline_guard.sh` for installation as
  `/userdata/init.d/S20jetkvm-baseline-guard`. The guard rejects
  non-`jetkvm-android-support` default/update binaries, quarantines them, and
  restores `/userdata/jetkvm/bin/jetkvm_app.android-support-current`.
- The same guard also forces `/userdata/kvm_config.json` auto-update settings
  off on boot: `auto_update_enabled=false` and `include_pre_release=false`.
- The guard quarantines `/userdata/jetkvm/update_system.tar` as
  `system-ota-disabled` so a leftover system OTA payload cannot be applied
  accidentally on the local test device.
- Tightened `scripts/dev_deploy.sh` so `--install` refuses to promote a default
  app unless it is built from `jetkvm-android-support` and contains the
  `jetkvm-android-support` build marker. Debug deploys are unaffected.
- Installed the guard on the JetKVM and verified it blocks a known stock
  `jetkvm_app.update` artifact while preserving the Android-support default.

## 2026-05-12 Companion Pulse Fix

- Fixed the companion so opening the settings UI no longer triggers the
  external-display presentation pulse. Service lifecycle starts now still send
  target metadata to JetKVM, but do not pulse the JetKVM display.
- Kept presentation pulses for real display-wake cases such as Android
  `SCREEN_ON`, JetKVM display add events, and non-lifecycle JetKVM peripheral
  changes.
- Added an explicit transparent presentation theme. The pulse presentation was
  already drawn transparent and 1x1, but using a transparent theme avoids the
  default presentation window background before content is attached. If Android
  clears the secondary display while creating a presentation, a residual blink
  may still be platform behavior.
- Installed the patched debug companion on the Pixel 8 and verified logs:
  `startup`/`startCommand` target reports occur without
  `target presentation pulse shown`.
