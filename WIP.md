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

