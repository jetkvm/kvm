import { useInterval } from "usehooks-ts";

import SidebarHeader from "@/components/SidebarHeader";
import { useRTCStore, useUiStore } from "@/hooks/stores";
import { someIterable } from "@/utils";

import { createChartArray, Metric } from "../Metric";
import { SettingsSectionHeader } from "../SettingsSectionHeader";

export default function ConnectionStatsSidebar() {
  const { sidebarView, setSidebarView } = useUiStore();
  const {
    mediaStream,
    peerConnection,
    inboundRtpStats: inboundVideoRtpStats,
    appendInboundRtpStats: appendInboundVideoRtpStats,
    candidatePairStats: iceCandidatePairStats,
    appendCandidatePairStats,
    appendLocalCandidateStats,
    appendRemoteCandidateStats,
    appendDiskDataChannelStats,
  } = useRTCStore();

  useInterval(function collectWebRTCStats() {
    (async () => {
      if (!mediaStream) return;

      const videoTrack = mediaStream.getVideoTracks()[0];
      if (!videoTrack) return;

      const stats = await peerConnection?.getStats();
      let successfulLocalCandidateId: string | null = null;
      let successfulRemoteCandidateId: string | null = null;

      stats?.forEach(report => {
        if (report.type === "inbound-rtp" && report.kind === "video") {
          appendInboundVideoRtpStats(report);
        } else if (report.type === "candidate-pair" && report.nominated) {
          if (report.state === "succeeded") {
            successfulLocalCandidateId = report.localCandidateId;
            successfulRemoteCandidateId = report.remoteCandidateId;
          }
          appendCandidatePairStats(report);
        } else if (report.type === "local-candidate") {
          // We only want to append the local candidate stats that were used in nominated candidate pair
          if (successfulLocalCandidateId === report.id) {
            appendLocalCandidateStats(report);
          }
        } else if (report.type === "remote-candidate") {
          if (successfulRemoteCandidateId === report.id) {
            appendRemoteCandidateStats(report);
          }
        } else if (report.type === "data-channel" && report.label === "disk") {
          appendDiskDataChannelStats(report);
        }
      });
    })();
  }, 500);

  const jitterBufferDelay = createChartArray(inboundVideoRtpStats, "jitterBufferDelay");
  const jitterBufferEmittedCount = createChartArray(
    inboundVideoRtpStats,
    "jitterBufferEmittedCount",
  );

  const jitterBufferAvgDelayData = jitterBufferDelay.map((d, idx) => {
    if (idx === 0) return { date: d.date, metric: null };
    const prevDelay = jitterBufferDelay[idx - 1]?.metric as number | null | undefined;
    const currDelay = d.metric as number | null | undefined;
    const prevCountEmitted =
      (jitterBufferEmittedCount[idx - 1]?.metric as number | null | undefined) ?? null;
    const currCountEmitted =
      (jitterBufferEmittedCount[idx]?.metric as number | null | undefined) ?? null;

    if (
      prevDelay == null ||
      currDelay == null ||
      prevCountEmitted == null ||
      currCountEmitted == null
    ) {
      return { date: d.date, metric: null };
    }

    const deltaDelay = currDelay - prevDelay;
    const deltaEmitted = currCountEmitted - prevCountEmitted;

    // Guard counter resets or no emitted frames
    if (deltaDelay < 0 || deltaEmitted <= 0) {
      return { date: d.date, metric: null };
    }

    const valueMs = Math.round((deltaDelay / deltaEmitted) * 1000);
    return { date: d.date, metric: valueMs };
  });

  return (
    <div className="grid h-full grid-rows-(--grid-headerBody) shadow-xs">
      <SidebarHeader title="Connection Stats" setSidebarView={setSidebarView} />
      <div className="h-full space-y-4 overflow-y-scroll bg-white px-4 py-2 pb-8 dark:bg-slate-900">
        <div className="space-y-4">
          {sidebarView === "connection-stats" && (
            <div className="space-y-8">
              {/* Connection Group */}
              <div className="space-y-3">
                <SettingsSectionHeader
                  title="Connection"
                  description="The connection between the client and the JetKVM."
                />
                <Metric
                  title="Round-Trip Time"
                  description="Round-trip time for the active ICE candidate pair between peers."
                  stream={iceCandidatePairStats}
                  metric="currentRoundTripTime"
                  map={x => ({
                    date: x.date,
                    metric: x.metric != null ? Math.round(x.metric * 1000) : null,
                  })}
                  domain={[0, 600]}
                  unit=" ms"
                />
              </div>

              {/* Video Group */}
              <div className="space-y-3">
                <SettingsSectionHeader
                  title="Video"
                  description="The video stream from the JetKVM to the client."
                />

                {/* RTP Jitter */}
                <Metric
                  title="Network Stability"
                  badge="Jitter"
                  badgeTheme="light"
                  description="How steady the flow of inbound video packets is across the network."
                  stream={inboundVideoRtpStats}
                  metric="jitter"
                  map={x => ({
                    date: x.date,
                    metric: x.metric != null ? Math.round(x.metric * 1000) : null,
                  })}
                  domain={[0, 10]}
                  unit=" ms"
                />

                {/* Playback Delay */}
                <Metric
                  title="Playback Delay"
                  description="Delay added by the jitter buffer to smooth playback when frames arrive unevenly."
                  badge="Jitter Buffer Avg. Delay"
                  badgeTheme="light"
                  data={jitterBufferAvgDelayData}
                  gate={inboundVideoRtpStats}
                  supported={
                    someIterable(
                      inboundVideoRtpStats,
                      ([, x]) => x.jitterBufferDelay != null,
                    ) &&
                    someIterable(
                      inboundVideoRtpStats,
                      ([, x]) => x.jitterBufferEmittedCount != null,
                    )
                  }
                  domain={[0, 30]}
                  unit=" ms"
                />

                {/* Packets Lost */}
                <Metric
                  title="Packets Lost"
                  description="Count of lost inbound video RTP packets."
                  stream={inboundVideoRtpStats}
                  metric="packetsLost"
                  domain={[0, 100]}
                  unit=" packets"
                />

                {/* Frames Per Second */}
                <Metric
                  title="Frames per second"
                  description="Number of inbound video frames displayed per second."
                  stream={inboundVideoRtpStats}
                  metric="framesPerSecond"
                  domain={[0, 80]}
                  unit=" fps"
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
