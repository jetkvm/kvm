# Bounded device stress test

`ra-stress.spec.ts` runs in the existing `remote-agent` project. It targets roughly
2–3 minutes of extra runtime, with one four-minute test timeout and no retries.
The phase timeouts are 55 seconds for connection churn, 80 seconds for mixed load,
and 75 seconds for handovers. The overall timeout also covers device setup and
recovery checks; remote-agent installation and Playwright teardown can add overhead.

Run just this test from `ui/` with the normal lab environment configured:

```sh
npm run test:e2e -- --project=remote-agent --grep @stress
```

Requires `JETKVM_URL`, `JETKVM_REMOTE_HOST`, a connected USB host running the remote
agent, and an active HDMI source. Device SSH is not required. As with the other
remote-agent tests, it sends real keyboard and mouse input to the test host.

The workload includes:

- Six complete connections and three closes after the device answers an offer,
  before the browser completes WebRTC setup.
- One minute of video with mouse movement at 25 Hz, 300 read-only RPCs in batches
  of five, and six short pastes checked against host keyboard events.
- Five consecutive takeovers to a second client and back via **Use Here** while
  both clients remain open. Each direction must deliver new video frames and
  keyboard/mouse press and release events, and show **Use Here** on the displaced
  client. A failed cycle fails the test without reconnecting or retrying it.

`stress-observations` attaches phase memory samples and individual RPC latencies.
Each RPC has a two-second timeout; recorded timings also include browser
communication overhead and are diagnostic only.
On the current Linux firmware, `/metrics` supplies process start time, Go heap
allocation, and resident memory. A changed or disappearing process-start metric
fails the test. Devices without these metrics still run the workloads and receive
an explicit annotation that the corresponding telemetry checks are unavailable.
P4 internal heap, PSRAM, and fragmentation measurements need a firmware telemetry
interface; the Linux metrics do not measure those pools.

Memory samples are diagnostic: pass/fail thresholds need calibration on the target
device. Initial and final single-session samples follow a two-second settling
period. No forced garbage collection or cache flushing is performed.
