import { useEffect, useState } from "react";
import { MdVolumeOff, MdVolumeUp, MdGraphicEq } from "react-icons/md";
import { LuActivity, LuSettings, LuSignal } from "react-icons/lu";

import { Button } from "@components/Button";
import { cx } from "@/cva.config";
import { useUiStore } from "@/hooks/stores";
import api from "@/api";

interface AudioConfig {
  Quality: number;
  Bitrate: number;
  SampleRate: number;
  Channels: number;
  FrameSize: string;
}

interface AudioMetrics {
  frames_received: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
}



const qualityLabels = {
  0: "Low (32kbps)",
  1: "Medium (64kbps)",
  2: "High (128kbps)",
  3: "Ultra (256kbps)"
};

export default function AudioControlPopover() {
  const [isMuted, setIsMuted] = useState(false);
  const [currentConfig, setCurrentConfig] = useState<AudioConfig | null>(null);

  const [metrics, setMetrics] = useState<AudioMetrics | null>(null);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const { toggleSidebarView } = useUiStore();

  // Load initial audio state
  useEffect(() => {
    loadAudioState();
    loadAudioMetrics();
    
    // Set up metrics refresh interval
    const metricsInterval = setInterval(loadAudioMetrics, 2000);
    return () => clearInterval(metricsInterval);
  }, []);

  const loadAudioState = async () => {
    try {
      // Load mute state
      const muteResp = await api.GET("/audio/mute");
      if (muteResp.ok) {
        const muteData = await muteResp.json();
        setIsMuted(!!muteData.muted);
      }

      // Load quality config
      const qualityResp = await api.GET("/audio/quality");
      if (qualityResp.ok) {
        const qualityData = await qualityResp.json();
        setCurrentConfig(qualityData.current);
      }
    } catch (error) {
      console.error("Failed to load audio state:", error);
    }
  };

  const loadAudioMetrics = async () => {
    try {
      const resp = await api.GET("/audio/metrics");
      if (resp.ok) {
        const data = await resp.json();
        setMetrics(data);
        // Consider connected if API call succeeds, regardless of frame count
        setIsConnected(true);
      } else {
        setIsConnected(false);
      }
    } catch (error) {
      console.error("Failed to load audio metrics:", error);
      setIsConnected(false);
    }
  };

  const handleToggleMute = async () => {
    setIsLoading(true);
    try {
      const resp = await api.POST("/audio/mute", { muted: !isMuted });
      if (resp.ok) {
        setIsMuted(!isMuted);
      }
    } catch (error) {
      console.error("Failed to toggle mute:", error);
    } finally {
      setIsLoading(false);
    }
  };

  const handleQualityChange = async (quality: number) => {
    setIsLoading(true);
    try {
      const resp = await api.POST("/audio/quality", { quality });
      if (resp.ok) {
        const data = await resp.json();
        setCurrentConfig(data.config);
      }
    } catch (error) {
      console.error("Failed to change audio quality:", error);
    } finally {
      setIsLoading(false);
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

  return (
    <div className="w-full max-w-md rounded-lg border border-slate-200 bg-white p-4 shadow-lg dark:border-slate-700 dark:bg-slate-800">
      <div className="space-y-4">
        {/* Header */}
        <div className="flex items-center justify-between">
          <h3 className="text-lg font-semibold text-slate-900 dark:text-slate-100">
            Audio Controls
          </h3>
          <div className="flex items-center gap-2">
            <div className={cx(
              "h-2 w-2 rounded-full",
              isConnected ? "bg-green-500" : "bg-red-500"
            )} />
            <span className="text-xs text-slate-500 dark:text-slate-400">
              {isConnected ? "Connected" : "Disconnected"}
            </span>
          </div>
        </div>

        {/* Mute Control */}
        <div className="flex items-center justify-between rounded-lg bg-slate-50 p-3 dark:bg-slate-700">
          <div className="flex items-center gap-3">
            {isMuted ? (
              <MdVolumeOff className="h-5 w-5 text-red-500" />
            ) : (
              <MdVolumeUp className="h-5 w-5 text-green-500" />
            )}
            <span className="font-medium text-slate-900 dark:text-slate-100">
              {isMuted ? "Muted" : "Unmuted"}
            </span>
          </div>
          <Button
            size="SM"
            theme={isMuted ? "danger" : "primary"}
            text={isMuted ? "Unmute" : "Mute"}
            onClick={handleToggleMute}
            disabled={isLoading}
          />
        </div>

        {/* Quality Settings */}
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <MdGraphicEq className="h-4 w-4 text-slate-600 dark:text-slate-400" />
            <span className="font-medium text-slate-900 dark:text-slate-100">
              Audio Quality
            </span>
          </div>
          
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(qualityLabels).map(([quality, label]) => (
              <button
                key={quality}
                onClick={() => handleQualityChange(parseInt(quality))}
                disabled={isLoading}
                className={cx(
                  "rounded-md border px-3 py-2 text-sm font-medium transition-colors",
                  currentConfig?.Quality === parseInt(quality)
                    ? "border-blue-500 bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300"
                    : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600",
                  isLoading && "opacity-50 cursor-not-allowed"
                )}
              >
                {label}
              </button>
            ))}
          </div>

          {currentConfig && (
            <div className="rounded-md bg-slate-50 p-2 text-xs text-slate-600 dark:bg-slate-700 dark:text-slate-400">
              <div className="grid grid-cols-2 gap-1">
                <span>Sample Rate: {currentConfig.SampleRate}Hz</span>
                <span>Channels: {currentConfig.Channels}</span>
                <span>Bitrate: {currentConfig.Bitrate}kbps</span>
                <span>Frame: {currentConfig.FrameSize}</span>
              </div>
            </div>
          )}
        </div>

        {/* Advanced Controls Toggle */}
        <button
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="flex w-full items-center justify-between rounded-md border border-slate-200 p-2 text-sm font-medium text-slate-700 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-700"
        >
          <div className="flex items-center gap-2">
            <LuSettings className="h-4 w-4" />
            <span>Advanced Metrics</span>
          </div>
          <span className={cx(
            "transition-transform",
            showAdvanced ? "rotate-180" : "rotate-0"
          )}>
            ▼
          </span>
        </button>

         {/* Advanced Metrics */}
        {showAdvanced && (
          <div className="space-y-3 rounded-lg border border-slate-200 p-3 dark:border-slate-600">
            <div className="flex items-center gap-2">
              <LuActivity className="h-4 w-4 text-slate-600 dark:text-slate-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Performance Metrics
              </span>
            </div>
            
            {metrics ? (
              <>
                <div className="grid grid-cols-2 gap-3 text-xs">
                  <div className="space-y-1">
                    <div className="text-slate-500 dark:text-slate-400">Frames Received</div>
                    <div className="font-mono text-green-600 dark:text-green-400">
                      {formatNumber(metrics.frames_received)}
                    </div>
                  </div>
                  
                  <div className="space-y-1">
                    <div className="text-slate-500 dark:text-slate-400">Frames Dropped</div>
                    <div className={cx(
                      "font-mono",
                      metrics.frames_dropped > 0 
                        ? "text-red-600 dark:text-red-400" 
                        : "text-green-600 dark:text-green-400"
                    )}>
                      {formatNumber(metrics.frames_dropped)}
                    </div>
                  </div>
                  
                  <div className="space-y-1">
                    <div className="text-slate-500 dark:text-slate-400">Data Processed</div>
                    <div className="font-mono text-blue-600 dark:text-blue-400">
                      {formatBytes(metrics.bytes_processed)}
                    </div>
                  </div>
                  
                  <div className="space-y-1">
                    <div className="text-slate-500 dark:text-slate-400">Connection Drops</div>
                    <div className={cx(
                      "font-mono",
                      metrics.connection_drops > 0 
                        ? "text-red-600 dark:text-red-400" 
                        : "text-green-600 dark:text-green-400"
                    )}>
                      {formatNumber(metrics.connection_drops)}
                    </div>
                  </div>
                </div>

                {metrics.frames_received > 0 && (
                  <div className="mt-3 rounded-md bg-slate-50 p-2 dark:bg-slate-700">
                    <div className="text-xs text-slate-500 dark:text-slate-400">Drop Rate</div>
                    <div className={cx(
                      "font-mono text-sm",
                      ((metrics.frames_dropped / metrics.frames_received) * 100) > 5
                        ? "text-red-600 dark:text-red-400"
                        : ((metrics.frames_dropped / metrics.frames_received) * 100) > 1
                        ? "text-yellow-600 dark:text-yellow-400"
                        : "text-green-600 dark:text-green-400"
                    )}>
                      {((metrics.frames_dropped / metrics.frames_received) * 100).toFixed(2)}%
                    </div>
                  </div>
                )}

                <div className="text-xs text-slate-500 dark:text-slate-400">
                  Last updated: {new Date().toLocaleTimeString()}
                </div>
              </>
            ) : (
              <div className="text-center py-4">
                <div className="text-sm text-slate-500 dark:text-slate-400">
                  Loading metrics...
                </div>
              </div>
            )}
          </div>
        )}

        {/* Audio Metrics Dashboard Button */}
        <div className="pt-2 border-t border-slate-200 dark:border-slate-600">
          <div className="flex justify-center">
            <button
              onClick={() => {
                toggleSidebarView("audio-metrics");
              }}
              className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600 transition-colors"
            >
              <LuSignal className="h-4 w-4 text-blue-500" />
              <span>View Full Audio Metrics</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}