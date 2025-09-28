import { useInterval } from "usehooks-ts";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
  return (
    <div className="grid h-full grid-rows-(--grid-headerBody) shadow-xs">
      <SidebarHeader title={t('Connection_Stats')} setSidebarView={setSidebarView} />
      <div className="h-full space-y-4 overflow-y-scroll bg-white px-4 py-2 pb-8 dark:bg-slate-900">
        <div className="space-y-4">
          {sidebarView === "connection-stats" && (
            <div className="space-y-8">
              {/* Connection Group */}
              <div className="space-y-3">
                <SettingsSectionHeader
                  title={t('Connection')}
                  description={t('The_connection_between_the_client_and_the_JetKVM')}
                />
                <Metric
                  title={t('Round-Trip_Time')}
                  description={t('Round-trip_time_for_the_active_ICE_candidate_pair_between_peers')}
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
                  title={t('Video')}
                  description={t('The_video_stream_from_the_JetKVM_to_the_client')}
                />

                {/* RTP Jitter */}
                <Metric
                  title={t('Network_Stability')}
                  badge={t('Jitter')}
                  badgeTheme="light"
                  description={t('How_steady_the_flow_of_inbound_video_packets_is_across_the_network')}
                  stream={inboundVideoRtpStats}
                  metric="jitter"
                  map={x => ({
                    date: x.date,
                    metric: x.metric != null ? Math.round(x.metric * 1000) : null,
                  })}
                  domain={[0, 10]}
                  unit={t('ms')}
                />

                {/* Playback Delay */}
                <Metric
                  title={t('Playback_Delay')}
                  description={t('Delay_added_by_the_jitter_buffer_to_smooth_playback_when_frames_arrive_unevenly')}
                  badge={t('Jitter_Buffer_Avg_Delay')}
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
                  unit={t('ms')}
                />

                {/* Packets Lost */}
                <Metric
                  title={t('Packets_Lost')}
                  description={t('Count_of_lost_inbound_video_RTP_packets')}
                  stream={inboundVideoRtpStats}
                  metric="packetsLost"
                  domain={[0, 100]}
                  unit={t('packets')}
                />

                {/* Frames Per Second */}
                <Metric
                  title={t('Frames_per_second')}
                  description={t('Number_of_inbound_video_frames_displayed_per_second')}
                  stream={inboundVideoRtpStats}
                  metric="framesPerSecond"
                  domain={[0, 80]}
                  unit={t('fps')}
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
