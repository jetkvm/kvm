# Video Pipeline Benchmark Changelog

## 2026-03-31: Pipeline latency optimizations

### Baseline (pre-optimization)

| Metric         | Value    |
| -------------- | -------- |
| Median latency | 136.0ms  |
| P95 latency    | 152.0ms  |
| Jitter buffer  | 59.7ms   |
| Frame drops    | 0        |
| Freezes        | 0        |
| Sample rate    | 56.4/sec |

### Changes

#### 1. Accurate frame duration over Unix socket IPC

**Files:** `internal/native/server.go`, `internal/native/proxy.go`, `internal/native/chan.go`, `internal/native/cgo_linux.go`

The native subprocess was discarding the frame capture timestamp when writing to the Unix socket, and the proxy was re-measuring frame duration from socket arrival times. This introduced IPC timing jitter into the RTP timestamps.

- Extended the socket wire protocol from `[4B size][data]` to `[4B size][8B duration_us][data]`
- Capture timestamp at CGO callback time (`time.Now()` before channel send)
- Compute inter-frame duration from capture timestamps, not dequeue/socket times
- Combined header + data writes into single `net.Buffers` writev syscall
- Increased socket send/receive buffers to 512KB

**Impact:** Jitter buffer 59.7ms → ~46ms (23% improvement)

#### 2. Playout-delay RTP header extension

**Files:** `playout_delay.go` (new), `webrtc.go`

Added a custom pion interceptor that attaches the `playout-delay` RTP header extension to every outgoing video packet. This tells Chrome to minimize its jitter buffer (min=0ms, max=0ms).

- Registered extension URI (`http://www.webrtc.org/experiments/rtp-hdrext/playout-delay`) in the MediaEngine
- Created custom interceptor that sets `{0x00, 0x00, 0x00}` extension on each outgoing RTP packet
- Configured custom MediaEngine + InterceptorRegistry with default interceptors plus playout-delay

**Impact:** Jitter buffer ~46ms → ~11ms (76% additional improvement)

#### 3. Browser-side playoutDelayHint

**File:** `ui/src/routes/devices.$id.tsx`

Set `playoutDelayHint = 0` on the `RTCRtpReceiver` in the `ontrack` handler, complementing the server-side extension.

### After all optimizations

| Metric         | Value    | Change                                           |
| -------------- | -------- | ------------------------------------------------ |
| Median latency | 89.5ms   | -34%                                             |
| P95 latency    | 118.5ms  | -22%                                             |
| Jitter buffer  | 11.2ms   | **-81%**                                         |
| Frame drops    | 0        | unchanged                                        |
| Freezes        | 0        | unchanged                                        |
| Sample rate    | 36.9/sec | -35% (fewer frames due to minimal jitter buffer) |

### Approaches tried but reverted

- **Decoupled WriteSample with buffered channel**: Added GC pressure from per-frame copies, causing regression
- **Decoupled WriteSample with pooled buffers**: No improvement, added complexity
- **Frame buffer pool in native subprocess**: Negligible impact on jitter
- **Fixed-rate RTP timestamps**: No improvement (jitter is physical, not timestamp-related)
- **Direct mode (no IPC)**: Better jitter (9ms) but worse median latency (~100ms) due to single-core contention
- **Reduced encoder stream buffers (3→2)**: No measurable impact
- **Removed socket buffer tuning**: Slightly worse results
