import { useInterval } from "usehooks-ts";

import SidebarHeader from "@/components/SidebarHeader";
import { GridCard } from "@/components/Card";
import { useRTCStore, useUiStore } from "@/hooks/stores";
import StatChart from "@/components/StatChart";

function createChartArray<T, K extends keyof T>(
  stream: Map<number, T>,
  metric: K,
): { date: number; stat: T[K] | null }[] {
  const stat = Array.from(stream).map(([key, stats]) => {
    return { date: key, stat: stats[metric] };
  });

  // Sort the dates to ensure they are in chronological order
  const sortedStat = stat.map(x => x.date).sort((a, b) => a - b);

  // Determine the earliest statistic date
  const earliestStat = sortedStat[0];

  // Current time in seconds since the Unix epoch
  const now = Math.floor(Date.now() / 1000);

  // Determine the starting point for the chart data
  const firstChartDate = earliestStat ? Math.min(earliestStat, now - 120) : now - 120;

  // Generate the chart array for the range between 'firstChartDate' and 'now'
  return Array.from({ length: now - firstChartDate }, (_, i) => {
    const currentDate = firstChartDate + i;
    return {
      date: currentDate,
      // Find the statistic for 'currentDate', or use the last known statistic if none exists for that date
      stat: stat.find(x => x.date === currentDate)?.stat ?? null,
    };
  });
}

export default function ConnectionStatsSidebar() {
  const inboundRtpStats = useRTCStore(state => state.inboundRtpStats);

  const candidatePairStats = useRTCStore(state => state.candidatePairStats);
  const setSidebarView = useUiStore(state => state.setSidebarView);

  function isMetricSupported<T, K extends keyof T>(
    stream: Map<number, T>,
    metric: K,
  ): boolean {
    return Array.from(stream).some(([, stat]) => stat[metric] !== undefined);
  }

  const appendInboundRtpStats = useRTCStore(state => state.appendInboundRtpStats);
  const appendIceCandidatePair = useRTCStore(state => state.appendCandidatePairStats);
  const appendDiskDataChannelStats = useRTCStore(
    state => state.appendDiskDataChannelStats,
  );
  const appendLocalCandidateStats = useRTCStore(state => state.appendLocalCandidateStats);
  const appendRemoteCandidateStats = useRTCStore(
    state => state.appendRemoteCandidateStats,
  );

  const peerConnection = useRTCStore(state => state.peerConnection);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const sidebarView = useUiStore(state => state.sidebarView);

  useInterval(function collectWebRTCStats() {
    (async () => {
      if (!mediaStream) return;
      const videoTrack = mediaStream.getVideoTracks()[0];
      if (!videoTrack) return;
      const stats = await peerConnection?.getStats();
      let successfulLocalCandidateId: string | null = null;
      let successfulRemoteCandidateId: string | null = null;

      stats?.forEach(report => {
        if (report.type === "inbound-rtp") {
          appendInboundRtpStats(report);
        } else if (report.type === "candidate-pair" && report.nominated) {
          if (report.state === "succeeded") {
            successfulLocalCandidateId = report.localCandidateId;
            successfulRemoteCandidateId = report.remoteCandidateId;
          }

          appendIceCandidatePair(report);
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

  return (
    <div className="grid h-full grid-rows-headerBody shadow-xs">
      <SidebarHeader title="Connection Stats" setSidebarView={setSidebarView} />
      <div className="h-full space-y-4 overflow-y-scroll bg-white px-4 py-2 pb-8 dark:bg-slate-900">
        <div className="space-y-4">
          {/*
            The entire sidebar component is always rendered, with a display none when not visible
            The charts below, need a height and width, otherwise they throw. So simply don't render them unless the thing is visible
          */}
          {sidebarView === "connection-stats" && (
            <div className="space-y-4">
              <div className="space-y-2">
                <div>
                  <h2 className="text-lg font-semibold text-black dark:text-white">
                    Packets Lost
                  </h2>
                  <p className="text-sm text-slate-700 dark:text-slate-300">
                    Number of data packets lost during transmission.
                  </p>
                </div>
                <GridCard>
                  <div className="flex h-[127px] w-full items-center justify-center text-sm text-slate-500">
                    {inboundRtpStats.size === 0 ? (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-slate-700">Waiting for data...</p>
                      </div>
                    ) : isMetricSupported(inboundRtpStats, "packetsLost") ? (
                      <StatChart
                        data={createChartArray(inboundRtpStats, "packetsLost")}
                        domain={[0, 100]}
                        unit=" packets"
                      />
                    ) : (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-black">Metric not supported</p>
                      </div>
                    )}
                  </div>
                </GridCard>
              </div>
              <div className="space-y-2">
                <div>
                  <h2 className="text-lg font-semibold text-black dark:text-white">
                    Round-Trip Time
                  </h2>
                  <p className="text-sm text-slate-700 dark:text-slate-300">
                    Time taken for data to travel from source to destination and back
                  </p>
                </div>
                <GridCard>
                  <div className="flex h-[127px] w-full items-center justify-center text-sm text-slate-500">
                    {inboundRtpStats.size === 0 ? (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-slate-700">Waiting for data...</p>
                      </div>
                    ) : isMetricSupported(candidatePairStats, "currentRoundTripTime") ? (
                      <StatChart
                        data={createChartArray(
                          candidatePairStats,
                          "currentRoundTripTime",
                        ).map(x => {
                          return {
                            date: x.date,
                            stat: x.stat ? Math.round(x.stat * 1000) : null,
                          };
                        })}
                        domain={[0, 600]}
                        unit=" ms"
                      />
                    ) : (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-black">Metric not supported</p>
                      </div>
                    )}
                  </div>
                </GridCard>
              </div>
              <div className="space-y-2">
                <div>
                  <h2 className="text-lg font-semibold text-black dark:text-white">
                    Jitter
                  </h2>
                  <p className="text-sm text-slate-700 dark:text-slate-300">
                    Variation in packet delay, affecting video smoothness.{" "}
                  </p>
                </div>
                <GridCard>
                  <div className="flex h-[127px] w-full items-center justify-center text-sm text-slate-500">
                    {inboundRtpStats.size === 0 ? (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-slate-700">Waiting for data...</p>
                      </div>
                    ) : (
                      <StatChart
                        data={createChartArray(inboundRtpStats, "jitter").map(x => {
                          return {
                            date: x.date,
                            stat: x.stat ? Math.round(x.stat * 1000) : null,
                          };
                        })}
                        domain={[0, 300]}
                        unit=" ms"
                      />
                    )}
                  </div>
                </GridCard>
              </div>
              <div className="space-y-2">
                <div>
                  <h2 className="text-lg font-semibold text-black dark:text-white">
                    Frames per second
                  </h2>
                  <p className="text-sm text-slate-700 dark:text-slate-300">
                    Number of video frames displayed per second.
                  </p>
                </div>
                <GridCard>
                  <div className="flex h-[127px] w-full items-center justify-center text-sm text-slate-500">
                    {inboundRtpStats.size === 0 ? (
                      <div className="flex flex-col items-center space-y-1">
                        <p className="text-slate-700">Waiting for data...</p>
                      </div>
                    ) : (
                      <StatChart
                        data={createChartArray(inboundRtpStats, "framesPerSecond").map(
                          x => {
                            return {
                              date: x.date,
                              stat: x.stat ? x.stat : null,
                            };
                          },
                        )}
                        domain={[0, 80]}
                        unit=" fps"
                      />
                    )}
                  </div>
                </GridCard>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
