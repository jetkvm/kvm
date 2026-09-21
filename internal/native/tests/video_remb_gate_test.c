#include <assert.h>
#include <string.h>
#include "../cgo/video_remb_gate.h"

/* Bytes the encoder produces during one second at a given bit rate. */
static uint64_t second_at(uint32_t bps) { return bps / 8U; }

int main(void)
{
    video_remb_gate s = {0};
    uint64_t sent = 0;
    const uint32_t ceiling = 4000000;
    const uint32_t *cuts = &s.decisions[VIDEO_REMB_CUT];
    const uint32_t *ignored = &s.decisions[VIDEO_REMB_IGNORED];
    const uint32_t *restores = &s.decisions[VIDEO_REMB_RESTORE];

    /* A new stream starts from the ceiling. */
    assert(video_remb_gate_step(&s, ceiling, sent, 0) == ceiling);
    assert(s.reason == VIDEO_REMB_CEILING);

    /* Static desktop: the sender is application-limited, and the receiver's
     * estimate (about 1.5x the tiny throughput) is not evidence against the
     * ceiling. */
    video_remb_gate_report(&s, 250000, 500);
    sent += second_at(160000);
    assert(video_remb_gate_step(&s, ceiling, sent, 1000) == ceiling);
    assert(s.reason == VIDEO_REMB_IGNORED);
    assert(s.achieved == 160000 && *ignored == 1 && *cuts == 0);

    /* Motion saturates the target and the receiver estimates less than what
     * was actually sent: a real constraint, cut to 90% of it. */
    video_remb_gate_report(&s, 2000000, 1500);
    sent += second_at(4000000);
    assert(video_remb_gate_step(&s, ceiling, sent, 2000) == 1800000);
    assert(s.reason == VIDEO_REMB_CUT && *cuts == 1);

    /* Re-reading the same report does not cut again; within the interval the
     * target is returned unchanged. */
    sent += second_at(1800000) / 2;
    assert(video_remb_gate_step(&s, ceiling, sent, 2500) == 1800000);
    sent += second_at(1800000) / 2;
    assert(video_remb_gate_step(&s, ceiling, sent, 3000) == 1800000);
    assert(s.reason == VIDEO_REMB_HOLD && *cuts == 1);

    /* While saturating, a higher estimate is followed directly. */
    video_remb_gate_report(&s, 2400000, 3500);
    sent += second_at(1800000);
    assert(video_remb_gate_step(&s, ceiling, sent, 4000) == 2160000);
    assert(s.reason == VIDEO_REMB_FOLLOW);

    /* A report between the achieved rate and the target while saturating is
     * neither evidence nor an application-limited report: hold. */
    video_remb_gate_report(&s, 2300000, 4500);
    sent += second_at(2200000);
    assert(video_remb_gate_step(&s, ceiling, sent, 5000) == 2160000);
    assert(s.reason == VIDEO_REMB_HOLD && *ignored == 1);

    /* Overuse reports below the achieved rate keep cutting while saturating. */
    video_remb_gate_report(&s, 1900000, 5500);
    sent += second_at(2200000);
    assert(video_remb_gate_step(&s, ceiling, sent, 6000) == 1710000);
    assert(*cuts == 2);

    /* The scene goes static again: reports below target are ignored and the
     * application-limited sender doubles back toward the ceiling every
     * second while the receiver keeps reporting. */
    video_remb_gate_report(&s, 300000, 6500);
    sent += second_at(160000);
    assert(video_remb_gate_step(&s, ceiling, sent, 7000) == 3420000);
    assert(s.reason == VIDEO_REMB_RESTORE && *restores == 1 && *ignored == 2);
    video_remb_gate_report(&s, 300000, 7500);
    sent += second_at(160000);
    assert(video_remb_gate_step(&s, ceiling, sent, 8000) == ceiling);
    assert(*restores == 2);

    /* A burst on a lossless link: the report is below the achieved rate but
     * the sender was far from its target, so it is not evidence of a
     * bottleneck. Lowering the ceiling is reported as such. */
    assert(video_remb_gate_step(&s, 1400000, sent, 8001) == 1400000);
    assert(s.reason == VIDEO_REMB_CEILING);
    video_remb_gate_report(&s, 550000, 8400);
    sent += second_at(900000);
    assert(video_remb_gate_step(&s, 1400000, sent, 9001) == 1400000);
    assert(s.reason == VIDEO_REMB_IGNORED && *cuts == 2);

    /* Raising the ceiling applies at once and starts a new interval, because
     * the bytes so far were sent under the old ceiling. */
    assert(video_remb_gate_step(&s, ceiling, sent, 9002) == ceiling);
    assert(s.reason == VIDEO_REMB_CEILING && s.step_ms == 9002 && s.sent_bytes == sent);

    /* Missing feedback holds the target even when the sender is idle. */
    video_remb_gate_report(&s, 1000000, 9500);
    sent += second_at(4000000);
    assert(video_remb_gate_step(&s, ceiling, sent, 10002) == 900000);
    sent += second_at(100000) * 6;
    assert(video_remb_gate_step(&s, ceiling, sent, 16001) == 900000);
    assert(s.reason == VIDEO_REMB_HOLD);

    /* The ceiling is always an upper bound, even within the interval, and
     * raising it applies immediately: the old evidence was against a lower rate. */
    assert(video_remb_gate_step(&s, 500000, sent, 16002) == 500000);
    assert(video_remb_gate_step(&s, ceiling, sent, 16003) == ceiling);
    assert(s.reason == VIDEO_REMB_CEILING);

    /* A one bit/s estimate while sending is a valid cut to the floor. */
    video_remb_gate_report(&s, 1, 16500);
    sent += second_at(4000000);
    assert(video_remb_gate_step(&s, ceiling, sent, 17003) == VIDEO_REMB_FLOOR_BPS);

    /* A byte counter that moves backwards counts as no traffic: the higher
     * estimate is followed and the idle sender restores on top of it. */
    video_remb_gate_report(&s, 3000000, 17500);
    assert(video_remb_gate_step(&s, ceiling, 0, 18003) == ceiling);
    assert(s.achieved == 0 && s.reason == VIDEO_REMB_RESTORE);
    /* A ceiling below the floor is still the bound. */
    assert(video_remb_gate_step(&s, 100000, 0, 18004) == 100000);
    assert(*cuts == 4 && *ignored == 4 && *restores == 3);

    /* Lowering the ceiling mid-interval also starts a new interval: bytes
     * sent under the 4 Mbit/s target must not look saturating against the
     * new 2 Mbit/s ceiling, so the pending report cannot cut yet. Once an
     * interval has run under the new ceiling, it can. */
    memset(&s, 0, sizeof(s));
    sent = 0;
    assert(video_remb_gate_step(&s, ceiling, sent, 20000) == ceiling);
    video_remb_gate_report(&s, 1500000, 20500);
    sent += second_at(1600000);
    assert(video_remb_gate_step(&s, 2000000, sent, 21000) == 2000000);
    assert(s.reason == VIDEO_REMB_CEILING && s.step_ms == 21000 && s.sent_bytes == sent);
    sent += second_at(1600000);
    assert(video_remb_gate_step(&s, 2000000, sent, 22000) == 1350000);
    assert(s.reason == VIDEO_REMB_CUT);

    /* A new stream starts from its own ceiling, with no old network constraints. */
    memset(&s, 0, sizeof(s));
    assert(video_remb_gate_step(&s, 2000000, 12345, 30000) == 2000000);
    assert(s.sent_bytes == 12345);

    assert(strcmp(video_remb_reason_name(VIDEO_REMB_CUT), "cut") == 0);
    assert(strcmp(video_remb_reason_name(VIDEO_REMB_REASON_COUNT), "unknown") == 0);
    return 0;
}
