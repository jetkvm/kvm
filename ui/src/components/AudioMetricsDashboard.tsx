import { useEffect, useState } from "react";
import { MdGraphicEq, MdSignalWifi4Bar, MdError, MdMic } from "react-icons/md";
import { LuActivity, LuClock, LuHardDrive, LuSettings, LuCpu, LuMemoryStick } from "react-icons/lu";

import { AudioLevelMeter } from "@components/AudioLevelMeter";
import { cx } from "@/cva.config";
import { useMicrophone } from "@/hooks/useMicrophone";
import { useAudioLevel } from "@/hooks/useAudioLevel";
import { useAudioEvents } from "@/hooks/useAudioEvents";
import api from "@/api";

interface AudioMetrics {
  frames_received: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
}

interface MicrophoneMetrics {
  frames_sent: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
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

const qualityLabels = {
  0: "Low",
  1: "Medium", 
  2: "High",
  3: "Ultra"
};

export default function AudioMetricsDashboard() {
  // Use WebSocket-based audio events for real-time updates
  const { 
    audioMetrics, 
    microphoneMetrics: wsMicrophoneMetrics, 
    isConnected: wsConnected 
  } = useAudioEvents();
  
  // Fallback state for when WebSocket is not connected
  const [fallbackMetrics, setFallbackMetrics] = useState<AudioMetrics | null>(null);
  const [fallbackMicrophoneMetrics, setFallbackMicrophoneMetrics] = useState<MicrophoneMetrics | null>(null);
  const [fallbackConnected, setFallbackConnected] = useState(false);
  
  // Process metrics state
  const [audioProcessMetrics, setAudioProcessMetrics] = useState<ProcessMetrics | null>(null);
  const [microphoneProcessMetrics, setMicrophoneProcessMetrics] = useState<ProcessMetrics | null>(null);
  
  // Historical data for histograms (last 60 data points, ~1 minute at 1s intervals)
  const [audioCpuHistory, setAudioCpuHistory] = useState<number[]>([]);
  const [audioMemoryHistory, setAudioMemoryHistory] = useState<number[]>([]);
  const [micCpuHistory, setMicCpuHistory] = useState<number[]>([]);
  const [micMemoryHistory, setMicMemoryHistory] = useState<number[]>([]);
  
  // Configuration state (these don't change frequently, so we can load them once)
  const [config, setConfig] = useState<AudioConfig | null>(null);
  const [microphoneConfig, setMicrophoneConfig] = useState<AudioConfig | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());
  
  // Use WebSocket data when available, fallback to polling data otherwise
  const metrics = wsConnected && audioMetrics !== null ? audioMetrics : fallbackMetrics;
  const microphoneMetrics = wsConnected && wsMicrophoneMetrics !== null ? wsMicrophoneMetrics : fallbackMicrophoneMetrics;
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
      // Load config
      const configResp = await api.GET("/audio/quality");
      if (configResp.ok) {
        const configData = await configResp.json();
        setConfig(configData.current);
      }

      // Load microphone config
      try {
        const micConfigResp = await api.GET("/microphone/quality");
        if (micConfigResp.ok) {
          const micConfigData = await micConfigResp.json();
          setMicrophoneConfig(micConfigData.current);
        }
      } catch (micConfigError) {
        console.debug("Microphone config not available:", micConfigError);
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
          setAudioProcessMetrics(audioProcessData);
          
          // Update historical data for histograms (keep last 60 points)
          if (audioProcessData.running) {
            setAudioCpuHistory(prev => {
              const newHistory = [...prev, audioProcessData.cpu_percent];
              return newHistory.slice(-60); // Keep last 60 data points
            });
            setAudioMemoryHistory(prev => {
              const newHistory = [...prev, audioProcessData.memory_percent];
              return newHistory.slice(-60);
            });
          }
        }
      } catch (audioProcessError) {
        console.debug("Audio process metrics not available:", audioProcessError);
      }

      // Load microphone metrics
      try {
        const micResp = await api.GET("/microphone/metrics");
        if (micResp.ok) {
          const micData = await micResp.json();
          setFallbackMicrophoneMetrics(micData);
        }
      } catch (micError) {
        // Microphone metrics might not be available, that's okay
        console.debug("Microphone metrics not available:", micError);
      }

      // Load microphone process metrics
      try {
        const micProcessResp = await api.GET("/microphone/process-metrics");
        if (micProcessResp.ok) {
          const micProcessData = await micProcessResp.json();
          setMicrophoneProcessMetrics(micProcessData);
          
          // Update historical data for histograms (keep last 60 points)
          if (micProcessData.running) {
            setMicCpuHistory(prev => {
              const newHistory = [...prev, micProcessData.cpu_percent];
              return newHistory.slice(-60); // Keep last 60 data points
            });
            setMicMemoryHistory(prev => {
              const newHistory = [...prev, micProcessData.memory_percent];
              return newHistory.slice(-60);
            });
          }
        }
      } catch (micProcessError) {
        console.debug("Microphone process metrics not available:", micProcessError);
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
    return ((metrics.frames_dropped / metrics.frames_received) * 100);
  };

  const formatMemory = (bytes: number) => {
    if (bytes === 0) return "0 MB";
    const mb = bytes / (1024 * 1024);
    if (mb < 1024) {
      return `${mb.toFixed(1)} MB`;
    }
    const gb = mb / 1024;
    return `${gb.toFixed(2)} GB`;
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

  // Histogram component for displaying historical data
  const Histogram = ({ data, title, unit, color }: { 
    data: number[], 
    title: string, 
    unit: string, 
    color: string 
  }) => {
    if (data.length === 0) return null;
    
    const maxValue = Math.max(...data, 1); // Avoid division by zero
    const minValue = Math.min(...data);
    const range = maxValue - minValue;
    
    return (
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-slate-700 dark:text-slate-300">
            {title}
          </span>
          <span className="text-xs text-slate-500 dark:text-slate-400">
            {data.length > 0 ? `${data[data.length - 1].toFixed(1)}${unit}` : `0${unit}`}
          </span>
        </div>
        <div className="flex items-end gap-0.5 h-16 bg-slate-50 dark:bg-slate-800 rounded p-2">
          {data.slice(-30).map((value, index) => { // Show last 30 points
            const height = range > 0 ? ((value - minValue) / range) * 100 : 0;
            return (
              <div
                key={index}
                className={cx(
                  "flex-1 rounded-sm transition-all duration-200",
                  color
                )}
                style={{ height: `${Math.max(height, 2)}%` }}
                title={`${value.toFixed(1)}${unit}`}
              />
            );
          })}
        </div>
        <div className="flex justify-between text-xs text-slate-400 dark:text-slate-500">
          <span>{minValue.toFixed(1)}{unit}</span>
          <span>{maxValue.toFixed(1)}{unit}</span>
        </div>
      </div>
    );
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
                  {qualityLabels[config.Quality as keyof typeof qualityLabels]}
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
                  {qualityLabels[microphoneConfig.Quality as keyof typeof qualityLabels]}
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
              <Histogram 
                data={audioCpuHistory} 
                title="CPU Usage" 
                unit="%" 
                color="bg-blue-500 dark:bg-blue-400" 
              />
              <Histogram 
                data={audioMemoryHistory} 
                title="Memory Usage" 
                unit="%" 
                color="bg-purple-500 dark:bg-purple-400" 
              />
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemory(audioProcessMetrics.memory_rss)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">RSS</div>
                </div>
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemory(audioProcessMetrics.memory_vms)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">VMS</div>
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
              <Histogram 
                data={micCpuHistory} 
                title="CPU Usage" 
                unit="%" 
                color="bg-green-500 dark:bg-green-400" 
              />
              <Histogram 
                data={micMemoryHistory} 
                title="Memory Usage" 
                unit="%" 
                color="bg-orange-500 dark:bg-orange-400" 
              />
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemory(microphoneProcessMetrics.memory_rss)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">RSS</div>
                </div>
                <div className="text-center p-2 bg-slate-50 dark:bg-slate-800 rounded">
                  <div className="font-medium text-slate-900 dark:text-slate-100">
                    {formatMemory(microphoneProcessMetrics.memory_vms)}
                  </div>
                  <div className="text-slate-500 dark:text-slate-400">VMS</div>
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
                  getDropRate() > 5 
                    ? "text-red-600 dark:text-red-400"
                    : getDropRate() > 1
                    ? "text-yellow-600 dark:text-yellow-400"
                    : "text-green-600 dark:text-green-400"
                )}>
                  {getDropRate().toFixed(2)}%
                </span>
              </div>
              <div className="mt-1 h-2 w-full rounded-full bg-slate-200 dark:bg-slate-600">
                <div 
                  className={cx(
                    "h-2 rounded-full transition-all duration-300",
                    getDropRate() > 5 
                      ? "bg-red-500"
                      : getDropRate() > 1
                      ? "bg-yellow-500"
                      : "bg-green-500"
                  )}
                  style={{ width: `${Math.min(getDropRate(), 100)}%` }}
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
                    (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100 : 0) > 5 
                      ? "text-red-600 dark:text-red-400"
                      : (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100 : 0) > 1
                      ? "text-yellow-600 dark:text-yellow-400"
                      : "text-green-600 dark:text-green-400"
                  )}>
                    {microphoneMetrics.frames_sent > 0 ? ((microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100).toFixed(2) : "0.00"}%
                  </span>
                </div>
                <div className="mt-1 h-2 w-full rounded-full bg-slate-200 dark:bg-slate-600">
                  <div 
                    className={cx(
                      "h-2 rounded-full transition-all duration-300",
                      (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100 : 0) > 5 
                        ? "bg-red-500"
                        : (microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100 : 0) > 1
                        ? "bg-yellow-500"
                        : "bg-green-500"
                    )}
                    style={{ 
                      width: `${Math.min(microphoneMetrics.frames_sent > 0 ? (microphoneMetrics.frames_dropped / microphoneMetrics.frames_sent) * 100 : 0, 100)}%` 
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