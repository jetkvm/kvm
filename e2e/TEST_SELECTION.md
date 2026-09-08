# Selecting and restoring hardware tests

The existing Playwright runner accepts `JETKVM_URL`, `JETKVM_REMOTE_HOST` and
`JETKVM_DEVICE_SSH=0` for devices without a shell. From `ui/`:

```sh
JETKVM_URL=http://device-under-test \
JETKVM_REMOTE_HOST=testuser@target-host \
JETKVM_DEVICE_SSH=0 \
./node_modules/.bin/playwright test --no-deps --grep-invert '@ssh|@serial'
```

`--no-deps` prevents excluded dependency projects from being added back to the
selection. Choose exclusions for the available equipment and capabilities.
H.265-specific assertions use `@h265`, custom EDID uses `@custom-edid`, and
Prometheus-dependent time-sync assertions use `@metrics`. Shared codec checks
exercise the codecs reported by the device.

Host suites require no-password access. If the device is protected, disable
protection before running them; host-suite setup no longer deletes configuration
through SSH. Browser authentication helpers accept `JETKVM_PASSWORD` for an
existing password and report unavailable shell recovery explicitly.

Shared host fixtures capture USB identity/classes, emulation, audio, mounted
media and keyboard macros, configure the test devices, then restore the captured
state. Cleanup attempts independent settings even if one restoration fails and
reports those failures. Use one browser/test owner at a time.

Without device SSH, upload verification reads the explicitly identified USB
block device through the connected host and checks its SHA-256. The host needs
Python 3 and noninteractive sudo for that read. Ambiguous devices and short reads
fail instead of silently accepting an incomplete image.

Static checks:

```sh
# From ui/
./node_modules/.bin/tsc -p tsconfig.e2e.json
./node_modules/.bin/oxlint -c .oxlintrc.json e2e --tsconfig tsconfig.e2e.json
# From the repository root
python3 -m unittest discover -s e2e/remote-agent -p 'test_*.py'
```

The custom-NTP case runs a temporary UDP responder on `JETKVM_REMOTE_HOST` and
checks that the device queries it before and after reboot. The host name must
resolve from the device, or use its IPv4 address. Python 3, noninteractive sudo
and a free UDP port 123 are required on that host. The responder accepts requests
only from the configured device IP and exits when its owning SSH connection
closes, with a five-minute maximum lifetime. Public fallback is disabled during
this case, and original network settings are restored in cleanup. No device SSH
or Prometheus metrics are required.
