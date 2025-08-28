import { useEffect, useState } from "react";
import { MdGraphicEq, MdSignalWifi4Bar, MdError, MdMic } from "react-icons/md";
import { LuActivity, LuClock, LuHardDrive, LuSettings, LuCpu, LuMemoryStick } from "react-icons/lu";

import { AudioLevelMeter } from "@components/AudioLevelMeter";
import StatChart from "@components/StatChart";
import LatencyHistogram from "@components/charts/LatencyHistogram";
import { cx } from "@/cva.config";
import { useMicrophone } from "@/hooks/useMicrophone";
import { useAudioLevel } from "@/hooks/useAudioLevel";
import { useAudioEvents } from "@/hooks/useAudioEvents";
import api from "@/api";
import { AUDIO_CONFIG } from "@/config/constants";
import audioQualityService from "@/services/audioQualityService";

interface LatencyHistogramData {
  buckets: number[]; // Bucket boundaries in milliseconds
  counts: number[];  // Count for each bucket
}

interface AudioMetrics {
  frames_received: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
  latency_histogram?: LatencyHistogramData;
}

interface MicrophoneMetrics {
  frames_sent: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
  latency_histogram?: LatencyHistogramData;
}

interface ProcessMetrics {
  cpu_percent: number;
  memory_percent: number;
  memory_rss: number;
  memory_vms: number;
  running: boolean;
}

interface AudioConfig {
  Quality: number;
  Bitrate: number;
  SampleRate: number;
  Channels: number;
  FrameSize: string;
}

// Quality labels will be managed by the audio quality service
const getQualityLabels = () => audioQualityService.getQualityLabels();

// Format percentage values to 2 decimal places
function formatPercentage(value: number | null | undefined): string {
  if (value === null || value === undefined || isNaN(value)) {
    return "0.00%";
  }
  return `${value.toFixed(2)}%`;
}

function formatMemoryMB(rssBytes: number | null | undefined): string {
  if (rssBytes === null || rssBytes === undefined || isNaN(rssBytes)) {
    return "0.00 MB";
  }
  const mb = rssBytes / (1024 * 1024);
  return `${mb.toFixed(2)} MB`;
}

// Default system memory estimate in MB (will be replaced by actual value from backend)
const DEFAULT_SYSTEM_MEMORY_MB = 4096; // 4GB default

// Create chart array similar to connectionStats.tsx
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

export default function AudioMetricsDashboard() {
  // System memory state
  const [systemMemoryMB, setSystemMemoryMB] = useState(DEFAULT_SYSTEM_MEMORY_MB);

  // Use WebSocket-based audio events for real-time updates
  const { 
    audioMetrics, 
    microphoneMetrics: wsMicrophoneMetrics, 
    audioProcessMetrics: wsAudioProcessMetrics,
    microphoneProcessMetrics: wsMicrophoneProcessMetrics,
    isConnected: wsConnected 
  } = useAudioEvents();

  // Fetch system memory information on component mount
  useEffect(() => {
    const fetchSystemMemory = async () => {
      try {
        const response = await api.GET('/system/memory');
        const data = await response.json();
        setSystemMemoryMB(data.total_memory_mb);
      } catch {
        // Failed to fetch system memory, using default
      }
    };
    fetchSystemMemory();
  }, []);

  // Update historical data when WebSocket process metrics are received
   useEffect(() => {
     if (wsConnected && wsAudioProcessMetrics && wsAudioProcessMetrics.running) {
       const now = Math.floor(Date.now() / 1000); // Convert to seconds for StatChart
       // Validate that now is a valid number
       if (isNaN(now)) return;
       
       const cpuStat = isNaN(wsAudioProcessMetrics.cpu_percent) ? null : wsAudioProcessMetrics.cpu_percent;
       
       setAudioCpuStats(prev => {
         const newMap = new Map(prev);
         newMap.set(now, { cpu_percent: cpuStat });
         // Keep only last 120 seconds of data for memory management
         const cutoff = now - 120;
         for (const [key] of newMap) {
           if (key < cutoff) newMap.delete(key);
         }
         return newMap;
       });
       
       setAudioMemoryStats(prev => {
         const newMap = new Map(prev);
         const memoryRss = isNaN(wsAudioProcessMetrics.memory_rss) ? null : wsAudioProcessMetrics.memory_rss;
         newMap.set(now, { memory_rss: memoryRss });
         // Keep only last 120 seconds of data for memory management
         const cutoff = now - 120;
         for (const [key] of newMap) {
           if (key < cutoff) newMap.delete(key);
         }
         return newMap;
       });
     }
   }, [wsConnected, wsAudioProcessMetrics]);

   useEffect(() => {
     if (wsConnected && wsMicrophoneProcessMetrics) {
       const now = Math.floor(Date.now() / 1000); // Convert to seconds for StatChart
       // Validate that now is a valid number
       if (isNaN(now)) return;
       
       const cpuStat = isNaN(wsMicrophoneProcessMetrics.cpu_percent) ? null : wsMicrophoneProcessMetrics.cpu_percent;
       
       setMicCpuStats(prev => {
         const newMap = new Map(prev);
         newMap.set(now, { cpu_percent: cpuStat });
         // Keep only last 120 seconds of data for memory management
         const cutoff = now - 120;
         for (const [key] of newMap) {
           if (key < cutoff) newMap.delete(key);
         }
         return newMap;
       });
       
       setMicMemoryStats(prev => {
         const newMap = new Map(prev);
         const memoryRss = isNaN(wsMicrophoneProcessMetrics.memory_rss) ? null : wsMicrophoneProcessMetrics.memory_rss;
         newMap.set(now, { memory_rss: memoryRss });
         // Keep only last 120 seconds of data for memory management
         const cutoff = now - 120;
         for (const [key] of newMap) {
           if (key < cutoff) newMap.delete(key);
         }
         return newMap;
       });
     }
   }, [wsConnected, wsMicrophoneProcessMetrics]);
  
  // Fallback state for when WebSocket is not connected
  const [fallbackMetrics, setFallbackMetrics] = useState<AudioMetrics | null>(null);
  const [fallbackMicrophoneMetrics, setFallbackMicrophoneMetrics] = useState<MicrophoneMetrics | null>(null);
  const [fallbackConnected, setFallbackConnected] = useState(false);
  
  // Process metrics state (fallback for when WebSocket is not connected)
  const [fallbackAudioProcessMetrics, setFallbackAudioProcessMetrics] = useState<ProcessMetrics | null>(null);
  const [fallbackMicrophoneProcessMetrics, setFallbackMicrophoneProcessMetrics] = useState<ProcessMetrics | null>(null);
  
  // Historical data for charts using Maps for better memory management
  const [audioCpuStats, setAudioCpuStats] = useState<Map<number, { cpu_percent: number | null }>>(new Map());
  const [audioMemoryStats, setAudioMemoryStats] = useState<Map<number, { memory_rss: number | null }>>(new Map());
  const [micCpuStats, setMicCpuStats] = useState<Map<number, { cpu_percent: number | null }>>(new Map());
  const [micMemoryStats, setMicMemoryStats] = useState<Map<number, { memory_rss: number | null }>>(new Map());
  
  // Configuration state (these don't change frequently, so we can load them once)
  const [config, setConfig] = useState<AudioConfig | null>(null);
  const [microphoneConfig, setMicrophoneConfig] = useState<AudioConfig | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());
  
  // Use WebSocket data when available, fallback to polling data otherwise
  const metrics = wsConnected && audioMetrics !== null ? audioMetrics : fallbackMetrics;
  const microphoneMetrics = wsConnected && wsMicrophoneMetrics !== null ? wsMicrophoneMetrics : fallbackMicrophoneMetrics;
  const audioProcessMetrics = wsConnected && wsAudioProcessMetrics !== null ? wsAudioProcessMetrics : fallbackAudioProcessMetrics;
  const microphoneProcessMetrics = wsConnected && wsMicrophoneProcessMetrics !== null ? wsMicrophoneProcessMetrics : fallbackMicrophoneProcessMetrics;
  const isConnected = wsConnected ? wsConnected : fallbackConnected;
  
  // Microphone state for audio level monitoring
  const { isMicrophoneActive, isMicrophoneMuted, microphoneStream } = useMicrophone();
  const { audioLevel, isAnalyzing } = useAudioLevel(
  isMicrophoneActive ? microphoneStream : null,
  {
  enabled: isMicrophoneActive,
  updateInterval: 120,
  });

  useEffect(() => {
    // Load initial configuration (only once)
    loadAudioConfig();
    
    // Set up fallback polling only when WebSocket is not connected
    if (!wsConnected) {
      loadAudioData();
      const interval = setInterval(loadAudioData, 1000);
      return () => clearInterval(interval);
    }
  }, [wsConnected]);

  const loadAudioConfig = async () => {
    try {
      // Use centralized audio quality service
      const { audio, microphone } = await audioQualityService.loadAllConfigurations();

      if (audio) {
        setConfig(audio.current);
      }

      if (microphone) {
        setMicrophoneConfig(microphone.current);
      }
    } catch (error) {
      console.error("Failed to load audio config:", error);
    }
  };

  const loadAudioData = async () => {
    try {
      // Load metrics
      const metricsResp = await api.GET("/audio/metrics");
      if (metricsResp.ok) {
        const metricsData = await metricsResp.json();
        setFallbackMetrics(metricsData);
        // Consider connected if API call succeeds, regardless of frame count
        setFallbackConnected(true);
        setLastUpdate(new Date());
      } else {
        setFallbackConnected(false);
      }

      // Load audio process metrics
      try {
        const audioProcessResp = await api.GET("/audio/process-metrics");
        if (audioProcessResp.ok) {
          const audioProcessData = await audioProcessResp.json();
          setFallbackAudioProcessMetrics(audioProcessData);
          
          // Update historical data for charts (keep last 120 seconds)
          if (audioProcessData.running) {
            const now = Math.floor(Date.now() / 1000); // Convert to seconds for StatChart
            // Validate that now is a valid number
            if (isNaN(now)) return;
            
            const cpuStat = isNaN(audioProcessData.cpu_percent) ? null : audioProcessData.cpu_percent;
            const memoryRss = isNaN(audioProcessData.memory_rss) ? null : audioProcessData.memory_rss;
            
            setAudioCpuStats(prev => {
              const newMap = new Map(prev);
              newMap.set(now, { cpu_percent: cpuStat });
              // Keep only last 120 seconds of data for memory management
              const cutoff = now - 120;
              for (const [key] of newMap) {
                if (key < cutoff) newMap.delete(key);
              }
              return newMap;
            });
            
            setAudioMemoryStats(prev => {
              const newMap = new Map(prev);
              newMap.set(now, { memory_rss: memoryRss });
              // Keep only last 120 seconds of data for memory management
              const cutoff = now - 120;
              for (const [key] of newMap) {
                if (key < cutoff) newMap.delete(key);
              }
              return newMap;
            });
          }
        }
      } catch {
        // Audio process metrics not available
      }

      // Load microphone metrics
      try {
        const micResp = await api.GET("/microphone/metrics");
        if (micResp.ok) {
          const micData = await micResp.json();
          setFallbackMicrophoneMetrics(micData);
        }
      } catch {
        // Microphone metrics might not be available, that's okay
        // Microphone metrics not available
      }

      // Load microphone process metrics
      try {
        const micProcessResp = await api.GET("/microphone/process-metrics");
        if (micProcessResp.ok) {
          const micProcessData = await micProcessResp.json();
          setFallbackMicrophoneProcessMetrics(micProcessData);
          
          // Update historical data for charts (keep last 120 seconds)
          const now = Math.floor(Date.now() / 1000); // Convert to seconds for StatChart
          // Validate that now is a valid number
          if (isNaN(now)) return;
          
          const cpuStat = isNaN(micProcessData.cpu_percent) ? null : micProcessData.cpu_percent;
          const memoryRss = isNaN(micProcessData.memory_rss) ? null : micProcessData.memory_rss;
          
          setMicCpuStats(prev => {
            const newMap = new Map(prev);
            newMap.set(now, { cpu_percent: cpuStat });
            // Keep only last 120 seconds of data for memory management
            const cutoff = now - 120;
            for (const [key] of newMap) {
              if (key < cutoff) newMap.delete(key);
            }
            return newMap;
          });
          
          setMicMemoryStats(prev => {
            const newMap = new Map(prev);
            newMap.set(now, { memory_rss: memoryRss });
            // Keep only last 120 seconds of data for memory management
            const cutoff = now - 120;
            for (const [key] of newMap) {
              if (key < cutoff) newMap.delete(key);
            }
            return newMap;
          });
        }
      } catch {
        // Microphone process metrics not available
      }
    } catch (error) {
      console.error("Failed to load audio data:", error);
      setFallbackConnected(false);
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const formatNumber = (num: number) => {
    return new Intl.NumberFormat().format(num);
  };

  const getDropRate = () => {
    if (!metrics || metrics.frames_received === 0) return 0;
    return ((metrics.frames_dropped / metrics.frames_received) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER);
  };





  const getQualityColor = (quality: number) => {
    switch (quality) {
      case 0: return "text-yellow-600 dark:text-yellow-400";
      case 1: return "text-blue-600 dark:text-blue-400";
      case 2: return "text-green-600 dark:text-green-400";
      case 3: return "text-purple-600 dark:text-purple-400";
      default: return "text-slate-600 dark:text-slate-400";
    }
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <MdGraphicEq className="h-5 w-5 text-blue-600 dark:text-blue-400" />
          <h3 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
            Audio Metrics
          </h3>
        </div>
        <div className="flex items-center gap-2">
          <div className={cx(
            "h-2 w-2 rounded-full",
            isConnected ? "bg-green-500" : "bg-red-500"
          )} />
          <span className="text-xs text-slate-500 dark:text-slate-400">
            {isConnected ? "Active" : "Inactive"}
          </span>
        </div>
      </div>

      {/* Current Configuration */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {config && (
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-2 flex items-center gap-2">
              <LuSettings className="h-4 w-4 text-blue-600 dark:text-blue-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Audio Output Config
              </span>
            </div>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Quality:</span>
                <span className={cx("font-medium", getQualityColor(config.Quality))}>
                  {getQualityLabels()[config.Quality]}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Bitrate:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {config.Bitrate}kbps
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Sample Rate:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {config.SampleRate}Hz
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Channels:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {config.Channels}
                </span>
              </div>
            </div>
          </div>
        )}

        {microphoneConfig && (
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-2 flex items-center gap-2">
              <MdMic className="h-4 w-4 text-green-600 dark:text-green-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Microphone Input Config
              </span>
            </div>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Quality:</span>
                <span className={cx("font-medium", getQualityColor(microphoneConfig.Quality))}>
                  {getQualityLabels()[microphoneConfig.Quality]}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Bitrate:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {microphoneConfig.Bitrate}kbps
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Sample Rate:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {microphoneConfig.SampleRate}Hz
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500 dark:text-slate-400">Channels:</span>
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  {microphoneConfig.Channels}
                </span>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Latency Histograms */}
      {metrics && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <LatencyHistogram
            data={audioMetrics?.latency_histogram}
            title="Audio Output Latency Distribution"
            height={180}
            className=""
          />
          {microphoneMetrics && (
            <LatencyHistogram
              data={microphoneMetrics.latency_histogram}
              title="Microphone Input Latency Distribution"
              height={180}
              className=""
            />
          )}
        </div>
      )}

      {/* Subprocess Resource Usage - Histogram View */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Audio Output Subprocess */}
        {audioProcessMetrics && (
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-3 flex items-center gap-2">
              <LuCpu className="h-4 w-4 text-blue-600 dark:text-blue-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Audio Output Process
              </span>
              <div className={cx(
                "h-2 w-2 rounded-full ml-auto",
                audioProcessMetrics.running ? "bg-green-500" : "bg-red-500"
              )} />
            </div>
            <div className="space-y-4">
              <div>
                <h4 className="text-sm font-medium text-slate-900 dark:text-slate-100 mb-2">CPU Usage</h4>
                <div className="h-24">
                  <StatChart
                    data={createChartArray(audioCpuStats, 'cpu_percent')}
                    unit="%"
                    domain={[0, 100]}
                  />
                </div>
              </div>
              <div>
                <h4 className="text-sm font-medium text-slate-900 dark:text-slate-100 mb-2">Memory Usage</h4>
                <div className="h-24">
                  <StatChart 
                    data={createChartArray(audioMemoryStats, 'memory_rss').map(item => ({
                      date: item.date,
                      stat: item.stat ? item.stat / (1024 * 1024) : null // Convert bytes to MB
                    }))}
                    unit="MB" 
                    domain={[0, systemMemoryMB]} 
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatPercentage(audioProcessMetrics.cpu_percent)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">CPU</div>
                </div>
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemoryMB(audioProcessMetrics.memory_rss)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">Memory</div>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Microphone Input Subprocess */}
        {microphoneProcessMetrics && (
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-3 flex items-center gap-2">
              <LuMemoryStick className="h-4 w-4 text-green-600 dark:text-green-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Microphone Input Process
              </span>
              <div className={cx(
                "h-2 w-2 rounded-full ml-auto",
                microphoneProcessMetrics.running ? "bg-green-500" : "bg-red-500"
              )} />
            </div>
            <div className="space-y-4">
              <div>
                <h4 className="text-sm font-medium text-slate-900 dark:text-slate-100 mb-2">CPU Usage</h4>
                <div className="h-24">
                  <StatChart 
                    data={createChartArray(micCpuStats, 'cpu_percent')} 
                    unit="%" 
                    domain={[0, 100]} 
                  />
                </div>
              </div>
              <div>
                <h4 className="text-sm font-medium text-slate-900 dark:text-slate-100 mb-2">Memory Usage</h4>
                <div className="h-24">
                  <StatChart 
                    data={createChartArray(micMemoryStats, 'memory_rss').map(item => ({
                      date: item.date,
                      stat: item.stat ? item.stat / (1024 * 1024) : null // Convert bytes to MB
                    }))}
                    unit="MB" 
                    domain={[0, systemMemoryMB]} 
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatPercentage(microphoneProcessMetrics.cpu_percent)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">CPU</div>
                </div>
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemoryMB(microphoneProcessMetrics.memory_rss)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">Memory</div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Performance Metrics */}
      {metrics && (
        <div className="space-y-3">
          {/* Audio Output Frames */}
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-2 flex items-center gap-2">
              <LuActivity className="h-4 w-4 text-green-600 dark:text-green-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Audio Output
              </span>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="text-center">
                <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                  {formatNumber(metrics.frames_received)}
                </div>
                <div className="text-xs text-slate-500 dark:text-slate-400">
                  Frames Received
                </div>
              </div>
              <div className="text-center">
                <div className={cx(
                  "text-2xl font-bold",
                  metrics.frames_dropped > 0 
                    ? "text-red-600 dark:text-red-400" 
                    : "text-green-600 dark:text-green-400"
                )}>
                  {formatNumber(metrics.frames_dropped)}
                </div>
                <div className="text-xs text-slate-500 dark:text-slate-400">
                  Frames Dropped
                </div>
              </div>
            </div>
            
            {/* Drop Rate */}
            <div className="mt-3 rounded-md bg-slate-50 p-2 dark:bg-slate-700">
              <div className="flex items-center justify-between">
                <span className="text-sm text-slate-600 dark:text-slate-400">
                  Drop Rate
                </span>
                <span className={cx(
                  "font-bold",
                  getDropRate() > AUDIO_CONFIG.DROP_RATE_CRITICAL_THRESHOLD 
                    ? "text-red-600 dark:text-red-400"
                    : getDropRate() > AUDIO_CONFIG.DROP_RATE_WARNING_THRESHOLD
                    ? "text-yellow-600 dark:text-yellow-400"
                    : "text-green-600 dark:text-green-400"
                )}>
                  {getDropRate().toFixed(AUDIO_CONFIG.PERCENTAGE_DECIMAL_PLACES)}%
                </span>
              </div>
              <div className="mt-1 h-2 w-full rounded-full bg-slate-200 dark:bg-slate-600">
                <div 
                  className={cx(
                    "h-2 rounded-full transition-all duration-300",
                    getDropRate() > AUDIO_CONFIG.DROP_RATE_CRITICAL_THRESHOLD 
                      ? "bg-red-500"
                      : getDropRate() > AUDIO_CONFIG.DROP_RATE_WARNING_THRESHOLD
                      ? "bg-yellow-500"
                      : "bg-green-500"
                  )}
                  style={{ width: `${Math.min(getDropRate(), AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE)}%` }}
                />
              </div>
            </div>
          </div>

          {/* Microphone Input Metrics */}
          {microphoneMetrics && (
            <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
              <div className="mb-2 flex items-center gap-2">
                <MdMic className="h-4 w-4 text-orange-600 dark:text-orange-400" />
                <span className="font-medium text-slate-900 dark:text-slate-100">
                  Microphone Input
                </span>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="text-center">
                  <div className="text-2xl font-bold text-orange-600 dark:text-orange-400">
                    {formatNumber(microphoneMetrics.frames_sent)}
                  </div>
                  <div className="text-xs text-slate-500 dark:text-slate-400">
                    Frames Sent
                  </div>
                </div>
                <div className="text-center">
                  <div className={cx(
                    "text-2xl font-bold",
                    microphoneMetrics.frames_dropped > 0 
                      ? "text-red-600 dark:text-red-400" 
                      : "text-green-600 dark:text-green-400"
                  )}>
                    {formatNumber(microphoneMetrics.frames_dropped)}
                  </div>
                  <div className="text-xs text-slate-500 dark:text-slate-400">
                    Frames Dropped
                  </div>
                </div>
              </div>
              
              {/* Microphone Drop Rate */}
              <div className="mt-3 rounded-md bg-slate-50 p-2 dark:bg-slate-700">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-slate-600 dark:text-slate-400">
                    Drop Rate
                  </span>
                  <span className={cx(
                    "font-bold",
                    (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER : 0) > AUDIO_CONFIG.DROP_RATE_CRITICAL_THRESHOLD 
                      ? "text-red-600 dark:text-red-400"
                      : (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER : 0) > AUDIO_CONFIG.DROP_RATE_WARNING_THRESHOLD
                      ? "text-yellow-600 dark:text-yellow-400"
                      : "text-green-600 dark:text-green-400"
                  )}>
                    {microphoneMetrics.frames_sent > 0 ? ((microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER).toFixed(AUDIO_CONFIG.PERCENTAGE_DECIMAL_PLACES) : "0.00"}%
                  </span>
                </div>
                <div className="mt-1 h-2 w-full rounded-full bg-slate-200 dark:bg-slate-600">
                  <div 
                    className={cx(
                      "h-2 rounded-full transition-all duration-300",
                      (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER : 0) > AUDIO_CONFIG.DROP_RATE_CRITICAL_THRESHOLD 
                        ? "bg-red-500"
                        : (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER : 0) > AUDIO_CONFIG.DROP_RATE_WARNING_THRESHOLD
                        ? "bg-yellow-500"
                        : "bg-green-500"
                    )}
                    style={{ 
                      width: `${Math.min(microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * AUDIO_CONFIG.PERCENTAGE_MULTIPLIER : 0, AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE)}%` 
                    }}
                  />
                </div>
              </div>
              
              {/* Microphone Audio Level */}
              {isMicrophoneActive && (
                <div className="mt-3 rounded-md bg-slate-50 p-2 dark:bg-slate-700">
                  <AudioLevelMeter
                    level={audioLevel}
                    isActive={isMicrophoneActive && !isMicrophoneMuted && isAnalyzing}
                    size="sm"
                    showLabel={true}
                  />
                </div>
              )}
              
              {/* Microphone Connection Health */}
              <div className="mt-3 rounded-md bg-slate-50 p-2 dark:bg-slate-700">
                <div className="mb-2 flex items-center gap-2">
                  <MdSignalWifi4Bar className="h-3 w-3 text-purple-600 dark:text-purple-400" />
                  <span className="text-sm font-medium text-slate-900 dark:text-slate-100">
                    Connection Health
                  </span>
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <span className="text-xs text-slate-500 dark:text-slate-400">
                      Connection Drops:
                    </span>
                    <span className={cx(
                      "text-xs font-medium",
                      microphoneMetrics.connection_drops > 0 
                        ? "text-red-600 dark:text-red-400" 
                        : "text-green-600 dark:text-green-400"
                    )}>
                      {formatNumber(microphoneMetrics.connection_drops)}
                    </span>
                  </div>
                  {microphoneMetrics.average_latency && (
                    <div className="flex justify-between">
                      <span className="text-xs text-slate-500 dark:text-slate-400">
                        Avg Latency:
                      </span>
                      <span className="text-xs font-medium text-slate-900 dark:text-slate-100">
                        {microphoneMetrics.average_latency}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Data Transfer */}
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-2 flex items-center gap-2">
              <LuHardDrive className="h-4 w-4 text-blue-600 dark:text-blue-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Data Transfer
              </span>
            </div>
            <div className="text-center">
              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                {formatBytes(metrics.bytes_processed)}
              </div>
              <div className="text-xs text-slate-500 dark:text-slate-400">
                Total Processed
              </div>
            </div>
          </div>

          {/* Connection Health */}
          <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-700">
            <div className="mb-2 flex items-center gap-2">
              <MdSignalWifi4Bar className="h-4 w-4 text-purple-600 dark:text-purple-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Connection Health
              </span>
            </div>
            <div className="space-y-2">
              <div className="flex justify-between">
                <span className="text-sm text-slate-500 dark:text-slate-400">
                  Connection Drops:
                </span>
                <span className={cx(
                  "font-medium",
                  metrics.connection_drops > 0 
                    ? "text-red-600 dark:text-red-400" 
                    : "text-green-600 dark:text-green-400"
                )}>
                  {formatNumber(metrics.connection_drops)}
                </span>
              </div>
              {metrics.average_latency && (
                <div className="flex justify-between">
                  <span className="text-sm text-slate-500 dark:text-slate-400">
                    Avg Latency:
                  </span>
                  <span className="font-medium text-slate-900 dark:text-slate-100">
                    {metrics.average_latency}
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Last Update */}
      <div className="flex items-center justify-center gap-2 text-xs text-slate-500 dark:text-slate-400">
        <LuClock className="h-3 w-3" />
        <span>Last updated: {lastUpdate.toLocaleTimeString()}</span>
      </div>

      {/* No Data State */}
      {!metrics && (
        <div className="flex flex-col items-center justify-center py-8 text-center">
          <MdError className="h-12 w-12 text-slate-400 dark:text-slate-600" />
          <h3 className="mt-2 text-sm font-medium text-slate-900 dark:text-slate-100">
            No Audio Data
          </h3>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Audio metrics will appear when audio streaming is active.
          </p>
        </div>
      )}
    </div>
  );
}