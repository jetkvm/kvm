#ifndef JETKVM_VIDEO_REMB_GATE_H
#define JETKVM_VIDEO_REMB_GATE_H

#include <stdbool.h>
#include <stdint.h>

// Receive-side REMB only describes traffic the sender actually put on the
// wire: it constrains a saturating sender and says nothing about an
// application-limited one. Chrome's estimate never exceeds about 1.5x the
// throughput it measured and drops to 0.85x that throughput on overuse, so a
// KVM desktop that sits idle most of the time would otherwise be capped at
// its idle rate and every scene change would decode at the floor. The gate
// counts a report as capacity evidence only when the sender was saturating
// its target and the estimate is below what was achieved; an
// application-limited sender returns toward the ceiling on fresh feedback.

typedef enum {
    VIDEO_REMB_HOLD = 0, // no new feedback, or a report that is neither evidence nor application-limited
    VIDEO_REMB_CUT,      // saturating sender, receiver estimate below the achieved rate
    VIDEO_REMB_IGNORED,  // receiver estimate below target from an application-limited sender
    VIDEO_REMB_FOLLOW,   // receiver estimate above target
    VIDEO_REMB_RESTORE,  // application-limited sender returning toward the ceiling
    VIDEO_REMB_CEILING,  // new stream or raised ceiling
    VIDEO_REMB_REASON_COUNT,
} video_remb_reason;

typedef struct {
    uint32_t target, ceiling, remb;
    uint32_t remb_reports;
    uint32_t consumed_remb;
    int64_t remb_ms, step_ms;
    uint64_t sent_bytes; // cumulative encoded bytes at the last evaluation
    uint32_t achieved;   // encoded rate over the last evaluation interval, bit/s
    uint32_t decisions[VIDEO_REMB_REASON_COUNT];
    video_remb_reason reason;
} video_remb_gate;

#define VIDEO_REMB_STEP_INTERVAL_MS 1000
#define VIDEO_REMB_REPORT_STALE_MS 5000
#define VIDEO_REMB_FLOOR_BPS 200000U
#define VIDEO_REMB_RESTORE_MIN_STEP_BPS 32000U

static inline uint32_t video_remb_min(uint32_t a, uint32_t b) { return a < b ? a : b; }
static inline uint32_t video_remb_max(uint32_t a, uint32_t b) { return a > b ? a : b; }

static inline void video_remb_gate_report(video_remb_gate *s, uint32_t bps, int64_t now_ms)
{
    s->remb = bps;
    s->remb_ms = now_ms;
    s->remb_reports++;
}

// Evaluate at most once per second. ceiling is the target the encoder would
// use without receiver feedback; sent_bytes is the cumulative count of
// encoded bytes. A zeroed gate initializes itself on the first call.
// Returns the target in bit/s, at or below the ceiling.
static inline uint32_t video_remb_gate_step(video_remb_gate *s, uint32_t ceiling,
                                            uint64_t sent_bytes, int64_t now_ms)
{
    if (ceiling > s->ceiling || s->target > ceiling) {
        // A new stream or a raised ceiling carries no network evidence
        // against the ceiling; a lowered one clamps the target. Either way
        // the bytes so far were sent under the old target, so the interval
        // starts over.
        s->ceiling = ceiling;
        s->target = ceiling;
        s->reason = VIDEO_REMB_CEILING;
        s->step_ms = now_ms;
        s->sent_bytes = sent_bytes;
        return ceiling;
    }
    s->ceiling = ceiling;
    const int64_t elapsed = now_ms - s->step_ms;
    if (elapsed < VIDEO_REMB_STEP_INTERVAL_MS) return s->target;
    const uint64_t delta = sent_bytes >= s->sent_bytes ? sent_bytes - s->sent_bytes : 0;
    s->achieved = (uint32_t)(delta * 8000ULL / (uint64_t)elapsed);
    s->sent_bytes = sent_bytes;
    s->step_ms = now_ms;

    // Rate control meets or overshoots its target whenever the scene demands
    // it; a full interval well below the target that was in force means the
    // scene, not the network, bounded the stream.
    const bool limited = s->achieved < s->target - s->target / 4;
    const bool fresh = s->remb_reports && now_ms - s->remb_ms <= VIDEO_REMB_REPORT_STALE_MS;
    const bool new_feedback = fresh && s->remb_reports != s->consumed_remb;
    s->consumed_remb = s->remb_reports;
    const uint32_t floor = video_remb_min(VIDEO_REMB_FLOOR_BPS, ceiling);
    uint32_t target = s->target;
    video_remb_reason reason = VIDEO_REMB_HOLD;
    if (new_feedback) {
        const uint32_t limit = video_remb_max(
            floor, video_remb_min(ceiling, (uint32_t)((uint64_t)s->remb * 9 / 10)));
        if (limit < target) {
            // A burst inside one window can leave the report below the
            // achieved rate on a lossless link; only a saturating sender
            // has tested the network.
            if (!limited && s->remb < s->achieved) {
                target = limit;
                reason = VIDEO_REMB_CUT;
            } else if (limited) {
                reason = VIDEO_REMB_IGNORED;
            }
        } else if (limit > target) {
            target = limit;
            reason = VIDEO_REMB_FOLLOW;
        }
        s->decisions[reason]++;
    }
    // Report freshness stands in for receiver liveness: missing feedback
    // holds the target rather than restoring it.
    if (fresh && limited && reason != VIDEO_REMB_CUT && target < ceiling) {
        target = video_remb_min(ceiling, video_remb_max(target * 2U, target + VIDEO_REMB_RESTORE_MIN_STEP_BPS));
        reason = VIDEO_REMB_RESTORE;
        s->decisions[reason]++;
    }
    s->target = target;
    s->reason = reason;
    return target;
}

static inline const char *video_remb_reason_name(video_remb_reason reason)
{
    static const char *const names[VIDEO_REMB_REASON_COUNT] = {
        "hold", "cut", "ignored", "follow", "restore", "ceiling",
    };
    return reason < VIDEO_REMB_REASON_COUNT ? names[reason] : "unknown";
}

#endif
