import { useEffect, useState } from "react";
import { MdVolumeOff, MdVolumeUp, MdGraphicEq, MdMic, MdMicOff, MdRefresh } from "react-icons/md";
import { LuActivity, LuSignal } from "react-icons/lu";

import { Button } from "@components/Button";
import { AudioLevelMeter } from "@components/AudioLevelMeter";
import { AudioConfigDisplay } from "@components/AudioConfigDisplay";
import { AudioStatusIndicator } from "@components/AudioStatusIndicator";
import { cx } from "@/cva.config";
import { useUiStore } from "@/hooks/stores";
import { useAudioDevices } from "@/hooks/useAudioDevices";
import { useAudioLevel } from "@/hooks/useAudioLevel";
import { useAudioEvents } from "@/hooks/useAudioEvents";
import api from "@/api";
import notifications from "@/notifications";
import audioQualityService from "@/services/audioQualityService";

// Type for microphone error
interface MicrophoneError {
  type: 'permission' | 'device' | 'network' | 'unknown';
  message: string;
}

// Type for microphone hook return value
interface MicrophoneHookReturn {
  isMicrophoneActive: boolean;
  isMicrophoneMuted: boolean;
  microphoneStream: MediaStream | null;
  startMicrophone: (deviceId?: string) => Promise<{ success: boolean; error?: MicrophoneError }>;
  stopMicrophone: () => Promise<{ success: boolean; error?: MicrophoneError }>;
  toggleMicrophoneMute: () => Promise<{ success: boolean; error?: MicrophoneError }>;
  syncMicrophoneState: () => Promise<void>;
  // Loading states
  isStarting: boolean;
  isStopping: boolean;
  isToggling: boolean;
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

interface AudioControlPopoverProps {
  microphone: MicrophoneHookReturn;
  open?: boolean; // whether the popover is open (controls analysis)
}

export default function AudioControlPopover({ microphone, open }: AudioControlPopoverProps) {
  const [currentConfig, setCurrentConfig] = useState<AudioConfig | null>(null);
  const [currentMicrophoneConfig, setCurrentMicrophoneConfig] = useState<AudioConfig | null>(null);

  const [isLoading, setIsLoading] = useState(false);
  
  // Add cache flags to prevent unnecessary API calls
  const [configsLoaded, setConfigsLoaded] = useState(false);
  
  // Add cooldown to prevent rapid clicking
  const [lastClickTime, setLastClickTime] = useState(0);
  const CLICK_COOLDOWN = 500; // 500ms cooldown between clicks
  
  // Use WebSocket-based audio events for real-time updates
  const { 
    audioMuted, 
    audioMetrics, 
    microphoneMetrics, 
    isConnected: wsConnected 
  } = useAudioEvents();
  
  // WebSocket-only implementation - no fallback polling
  
  // Microphone state from props
  const {
    isMicrophoneActive,
    isMicrophoneMuted,
    microphoneStream,
    startMicrophone,
    stopMicrophone,
    toggleMicrophoneMute,
    syncMicrophoneState,
    // Loading states
    isStarting,
    isStopping,
    isToggling,
  } = microphone;
  
  // Use WebSocket data exclusively - no polling fallback
  const isMuted = audioMuted ?? false;
  const metrics = audioMetrics;
  const micMetrics = microphoneMetrics;
  const isConnected = wsConnected;
  
  // Audio level monitoring - enable only when popover is open and microphone is active to save resources
  const analysisEnabled = (open ?? true) && isMicrophoneActive;
  const { audioLevel, isAnalyzing } = useAudioLevel(analysisEnabled ? microphoneStream : null, {
    enabled: analysisEnabled,
    updateInterval: 120, // 8-10 fps to reduce CPU without losing UX quality
  });
  
  // Audio devices
  const { 
    audioInputDevices, 
    audioOutputDevices, 
    selectedInputDevice, 
    selectedOutputDevice, 
    setSelectedInputDevice, 
    setSelectedOutputDevice,
    isLoading: devicesLoading,
    error: devicesError,
    refreshDevices 
  } = useAudioDevices();
  
  const { toggleSidebarView } = useUiStore();

  // Load initial configurations once - cache to prevent repeated calls
  useEffect(() => {
    if (!configsLoaded) {
      loadAudioConfigurations();
    }
  }, [configsLoaded]);

  // WebSocket-only implementation - sync microphone state when needed
  useEffect(() => {
    // Always sync microphone state, but debounce it
    const syncTimeout = setTimeout(() => {
      syncMicrophoneState();
    }, 500);
    
    return () => clearTimeout(syncTimeout);
  }, [syncMicrophoneState]);

  const loadAudioConfigurations = async () => {
    try {
      // Use centralized audio quality service
      const { audio, microphone } = await audioQualityService.loadAllConfigurations();

      if (audio) {
        setCurrentConfig(audio.current);
      }

      if (microphone) {
        setCurrentMicrophoneConfig(microphone.current);
      }
      
      setConfigsLoaded(true);
    } catch {
      // Failed to load audio configurations
    }
  };

  const handleToggleMute = async () => {
    setIsLoading(true);
    try {
      const resp = await api.POST("/audio/mute", { muted: !isMuted });
      if (!resp.ok) {
        // Failed to toggle mute
      }
      // WebSocket will handle the state update automatically
    } catch {
      // Failed to toggle mute
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
    } catch {
      // Failed to change audio quality
    } finally {
      setIsLoading(false);
    }
  };

  const handleMicrophoneQualityChange = async (quality: number) => {
    try {
      const resp = await api.POST("/microphone/quality", { quality });
      if (resp.ok) {
        const data = await resp.json();
        setCurrentMicrophoneConfig(data.config);
      }
    } catch {
      // Failed to change microphone quality
    }
  };

  const handleToggleMicrophone = async () => {
    const now = Date.now();
    
    // Prevent rapid clicking - if any operation is in progress or within cooldown, ignore the click
    if (isStarting || isStopping || isToggling || (now - lastClickTime < CLICK_COOLDOWN)) {
      return;
    }
    
    setLastClickTime(now);
    
    try {
      const result = isMicrophoneActive ? await stopMicrophone() : await startMicrophone(selectedInputDevice);
      if (!result.success && result.error) {
        notifications.error(result.error.message);
      }
    } catch {
      // Failed to toggle microphone
      notifications.error("An unexpected error occurred");
    }
  };

  const handleToggleMicrophoneMute = async () => {
    const now = Date.now();
    
    // Prevent rapid clicking - if any operation is in progress or within cooldown, ignore the click
    if (isStarting || isStopping || isToggling || (now - lastClickTime < CLICK_COOLDOWN)) {
      return;
    }
    
    setLastClickTime(now);
    
    try {
      const result = await toggleMicrophoneMute();
      if (!result.success && result.error) {
        notifications.error(result.error.message);
      }
    } catch {
      // Failed to toggle microphone mute
      notifications.error("Failed to toggle microphone mute");
    }
  };

  // Handle microphone device change
  const handleMicrophoneDeviceChange = async (deviceId: string) => {
    setSelectedInputDevice(deviceId);
    
    // If microphone is currently active, restart it with the new device
    if (isMicrophoneActive) {
      try {
        // Stop current microphone
        await stopMicrophone();
        // Start with new device
        const result = await startMicrophone(deviceId);
        if (!result.success && result.error) {
          notifications.error(result.error.message);
        }
      } catch {
        // Failed to change microphone device
        notifications.error("Failed to change microphone device");
      }
    }
  };

  const handleAudioOutputDeviceChange = async (deviceId: string) => {
    setSelectedOutputDevice(deviceId);
    
    // Find the video element and set the audio output device
    const videoElement = document.querySelector('video');
    if (videoElement && 'setSinkId' in videoElement) {
      try {
        await (videoElement as HTMLVideoElement & { setSinkId: (deviceId: string) => Promise<void> }).setSinkId(deviceId);
      } catch {
        // Failed to change audio output device
      }
    } else {
      // setSinkId not supported or video element not found
    }
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

        {/* Microphone Control */}
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <MdMic className="h-4 w-4 text-slate-600 dark:text-slate-400" />
            <span className="font-medium text-slate-900 dark:text-slate-100">
              Microphone Input
            </span>
          </div>
          
          <div className="flex items-center justify-between rounded-lg bg-slate-50 p-3 dark:bg-slate-700">
            <div className="flex items-center gap-3">
              {isMicrophoneActive ? (
                isMicrophoneMuted ? (
                  <MdMicOff className="h-5 w-5 text-yellow-500" />
                ) : (
                  <MdMic className="h-5 w-5 text-green-500" />
                )
              ) : (
                <MdMicOff className="h-5 w-5 text-red-500" />
              )}
              <span className="font-medium text-slate-900 dark:text-slate-100">
                {!isMicrophoneActive 
                  ? "Inactive" 
                  : isMicrophoneMuted 
                    ? "Muted" 
                    : "Active"
                }
              </span>
            </div>
            <div className="flex gap-2">
              <Button
                size="SM"
                theme={isMicrophoneActive ? "danger" : "primary"}
                text={
                  isStarting ? "Starting..." : 
                  isStopping ? "Stopping..." : 
                  isMicrophoneActive ? "Stop" : "Start"
                }
                onClick={handleToggleMicrophone}
                disabled={isStarting || isStopping || isToggling}
                loading={isStarting || isStopping}
              />
              {isMicrophoneActive && (
                <Button
                  size="SM"
                  theme={isMicrophoneMuted ? "danger" : "light"}
                  text={
                    isToggling ? (isMicrophoneMuted ? "Unmuting..." : "Muting...") :
                    isMicrophoneMuted ? "Unmute" : "Mute"
                  }
                  onClick={handleToggleMicrophoneMute}
                  disabled={isStarting || isStopping || isToggling}
                  loading={isToggling}
                />
              )}
            </div>
          </div>
          
          {/* Audio Level Meter */}
          {isMicrophoneActive && (
            <div className="rounded-lg bg-slate-50 p-3 dark:bg-slate-700">
              <AudioLevelMeter
                level={audioLevel}
                isActive={isMicrophoneActive && !isMicrophoneMuted && isAnalyzing}
                size="md"
                showLabel={true}
              />
              {/* Debug information */}
              <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                <div className="grid grid-cols-2 gap-1">
                  <span>Stream: {microphoneStream ? '✓' : '✗'}</span>
                  <span>Analyzing: {isAnalyzing ? '✓' : '✗'}</span>
                  <span>Active: {isMicrophoneActive ? '✓' : '✗'}</span>
                  <span>Muted: {isMicrophoneMuted ? '✓' : '✗'}</span>
                </div>
                {microphoneStream && (
                  <div className="mt-1">
                    Tracks: {microphoneStream.getAudioTracks().length}
                    {microphoneStream.getAudioTracks().length > 0 && (
                      <span className="ml-2">
                        (Enabled: {microphoneStream.getAudioTracks().filter((t: MediaStreamTrack) => t.enabled).length})
                      </span>
                    )}
                  </div>
                )}
                <button
                  onClick={syncMicrophoneState}
                  className="mt-1 text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Sync State
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Device Selection */}
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <MdMic className="h-4 w-4 text-slate-600 dark:text-slate-400" />
            <span className="font-medium text-slate-900 dark:text-slate-100">
              Audio Devices
            </span>
            {devicesLoading && (
              <div className="h-3 w-3 animate-spin rounded-full border border-slate-300 border-t-slate-600 dark:border-slate-600 dark:border-t-slate-300" />
            )}
          </div>
          
          {devicesError && (
            <div className="rounded-md bg-red-50 p-2 text-xs text-red-600 dark:bg-red-900/20 dark:text-red-400">
              {devicesError}
            </div>
          )}
          
          {/* Microphone Selection */}
          <div className="space-y-2">
            <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
              Microphone
            </label>
            <select
               value={selectedInputDevice}
               onChange={(e) => handleMicrophoneDeviceChange(e.target.value)}
               disabled={devicesLoading}
              className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:bg-slate-50 disabled:text-slate-500 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:focus:border-blue-400 dark:disabled:bg-slate-800"
            >
              {audioInputDevices.map((device) => (
                <option key={device.deviceId} value={device.deviceId}>
                  {device.label}
                </option>
              ))}
            </select>
            {isMicrophoneActive && (
               <p className="text-xs text-slate-500 dark:text-slate-400">
                 Changing device will restart the microphone
               </p>
             )}
          </div>
          
          {/* Speaker Selection */}
          <div className="space-y-2">
            <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
              Speaker
            </label>
            <select
              value={selectedOutputDevice}
              onChange={(e) => handleAudioOutputDeviceChange(e.target.value)}
              disabled={devicesLoading}
              className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:bg-slate-50 disabled:text-slate-500 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:focus:border-blue-400 dark:disabled:bg-slate-800"
            >
              {audioOutputDevices.map((device) => (
                <option key={device.deviceId} value={device.deviceId}>
                  {device.label}
                </option>
              ))}
            </select>
          </div>
          
          <button
            onClick={refreshDevices}
            disabled={devicesLoading}
            className="flex w-full items-center justify-center gap-2 rounded-md border border-slate-200 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-700"
          >
            <MdRefresh className={cx("h-4 w-4", devicesLoading && "animate-spin")} />
            Refresh Devices
          </button>
        </div>

        {/* Microphone Quality Settings */}
        {isMicrophoneActive && (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <MdMic className="h-4 w-4 text-slate-600 dark:text-slate-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Microphone Quality
              </span>
            </div>
            
            <div className="grid grid-cols-2 gap-2">
              {Object.entries(getQualityLabels()).map(([quality, label]) => (
                <button
                  key={`mic-${quality}`}
                  onClick={() => handleMicrophoneQualityChange(parseInt(quality))}
                  disabled={isStarting || isStopping || isToggling}
                  className={cx(
                    "rounded-md border px-3 py-2 text-sm font-medium transition-colors",
                    currentMicrophoneConfig?.Quality === parseInt(quality)
                      ? "border-green-500 bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300"
                      : "border-slate-200 bg-white text-slate-700 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600",
                    (isStarting || isStopping || isToggling) && "opacity-50 cursor-not-allowed"
                  )}
                >
                  {label}
                </button>
              ))}
            </div>

            {currentMicrophoneConfig && (
              <AudioConfigDisplay 
                config={currentMicrophoneConfig} 
                variant="success" 
              />
            )}
          </div>
        )}

        {/* Quality Settings */}
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <MdGraphicEq className="h-4 w-4 text-slate-600 dark:text-slate-400" />
            <span className="font-medium text-slate-900 dark:text-slate-100">
              Audio Output Quality
            </span>
          </div>
          
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(getQualityLabels()).map(([quality, label]) => (
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
            <AudioConfigDisplay 
              config={currentConfig} 
              variant="default" 
            />
          )}
        </div>

        {/* Quick Status Summary */}
        <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-600">
          <div className="flex items-center gap-2 mb-2">
            <LuActivity className="h-4 w-4 text-slate-600 dark:text-slate-400" />
            <span className="font-medium text-slate-900 dark:text-slate-100">
              Quick Status
            </span>
          </div>
          
          {metrics ? (
            <div className="grid grid-cols-2 gap-3 text-xs">
              <AudioStatusIndicator 
                metrics={metrics} 
                label="Audio Output" 
              />
              
              {micMetrics && (
                <AudioStatusIndicator 
                  metrics={micMetrics} 
                  label="Microphone" 
                />
              )}
            </div>
          ) : (
            <div className="text-center py-2">
              <div className="text-sm text-slate-500 dark:text-slate-400">
                No data available
              </div>
            </div>
          )}
        </div>

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