import { useEffect, useState } from "react";
import { MdVolumeOff, MdVolumeUp, MdGraphicEq, MdMic, MdMicOff, MdRefresh } from "react-icons/md";

import { Button } from "@components/Button";
import { cx } from "@/cva.config";
import { useAudioDevices } from "@/hooks/useAudioDevices";
import { useAudioEvents } from "@/hooks/useAudioEvents";
import { useJsonRpc, JsonRpcResponse } from "@/hooks/useJsonRpc";
import { useRTCStore } from "@/hooks/stores";
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
  // HTTP/HTTPS detection
  isHttpsRequired: boolean;
}

interface AudioConfig {
  Quality: number;
  Bitrate: number;
  SampleRate: number;
  Channels: number;
  FrameSize: string;
}

interface AudioControlPopoverProps {
  microphone: MicrophoneHookReturn;
}

export default function AudioControlPopover({ microphone }: AudioControlPopoverProps) {
  const [currentConfig, setCurrentConfig] = useState<AudioConfig | null>(null);

  const [isLoading, setIsLoading] = useState(false);
  
  // Add cache flags to prevent unnecessary API calls
  const [configsLoaded, setConfigsLoaded] = useState(false);
  
  // Add cooldown to prevent rapid clicking
  const [lastClickTime, setLastClickTime] = useState(0);
  const CLICK_COOLDOWN = 500; // 500ms cooldown between clicks
  
  // Use WebSocket-based audio events for real-time updates
  const { 
    audioMuted, 
    // microphoneState - now using hook state instead
    isConnected: wsConnected 
  } = useAudioEvents();
  
  // RPC for device communication (works both locally and via cloud)
  const { rpcDataChannel } = useRTCStore();
  const { send } = useJsonRpc();
  
  // Initialize audio quality service with RPC for cloud compatibility
  useEffect(() => {
    if (send) {
      audioQualityService.setRpcSend(send);
    }
  }, [send]);
  
  // WebSocket-only implementation - no fallback polling
  
  // Microphone state from props (keeping hook for legacy device operations)
  const {
    isMicrophoneActive: isMicrophoneActiveFromHook,
    startMicrophone,
    stopMicrophone,
    syncMicrophoneState,
    // Loading states
    isStarting,
    isStopping,
    isToggling,
    // HTTP/HTTPS detection
    isHttpsRequired,
  } = microphone;
  
  // Use WebSocket data exclusively - no polling fallback
  const isMuted = audioMuted ?? false;
  const isConnected = wsConnected;
  

  
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
      const { audio } = await audioQualityService.loadAllConfigurations();

      if (audio) {
        setCurrentConfig(audio.current);
      }
      
      setConfigsLoaded(true);
    } catch {
      // Failed to load audio configurations
    }
  };

  const handleToggleMute = async () => {
    const now = Date.now();
    
    // Prevent rapid clicking
    if (isLoading || (now - lastClickTime < CLICK_COOLDOWN)) {
      return;
    }
    
    setLastClickTime(now);
    setIsLoading(true);
    
    try {
      // Use RPC for device communication - works for both local and cloud
      if (rpcDataChannel?.readyState !== "open") {
        throw new Error("Device connection not available");
      }

      await new Promise<void>((resolve, reject) => {
        send("audioMute", { muted: !isMuted }, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            reject(new Error(resp.error.message));
          } else {
            resolve();
          }
        });
      });
      
      // WebSocket will handle the state update automatically
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Failed to toggle audio mute";
      notifications.error(errorMessage);
    } finally {
      setIsLoading(false);
    }
  };

  // Quality change handler removed - quality is now fixed at optimal settings

  const handleToggleMicrophoneEnable = async () => {
    const now = Date.now();
    
    // Prevent rapid clicking - if any operation is in progress or within cooldown, ignore the click
    if (isStarting || isStopping || isToggling || (now - lastClickTime < CLICK_COOLDOWN)) {
      return;
    }
    
    setLastClickTime(now);
    setIsLoading(true);
    
    try {
      if (isMicrophoneActiveFromHook) {
        // Disable: Use the hook's stopMicrophone which handles both RPC and local cleanup
        const result = await stopMicrophone();
        if (!result.success) {
          throw new Error(result.error?.message || "Failed to stop microphone");
        }
      } else {
        // Enable: Use the hook's startMicrophone which handles both RPC and local setup
        const result = await startMicrophone();
        if (!result.success) {
          throw new Error(result.error?.message || "Failed to start microphone");
        }
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Failed to toggle microphone";
      notifications.error(errorMessage);
    } finally {
      setIsLoading(false);
    }
  };

  // Handle microphone device change
  const handleMicrophoneDeviceChange = async (deviceId: string) => {
    // Don't process device changes for HTTPS-required placeholder
    if (deviceId === 'https-required') {
      return;
    }
    
    setSelectedInputDevice(deviceId);
    
    // If microphone is currently active, restart it with the new device
    if (isMicrophoneActiveFromHook) {
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
            theme={isMuted ? "primary" : "danger"}
            text={isMuted ? "Enable" : "Disable"}
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
              {isMicrophoneActiveFromHook ? (
                <MdMic className="h-5 w-5 text-green-500" />
              ) : (
                <MdMicOff className="h-5 w-5 text-red-500" />
              )}
              <span className="font-medium text-slate-900 dark:text-slate-100">
                {isMicrophoneActiveFromHook ? "Enabled" : "Disabled"}
              </span>
            </div>
            <Button
              size="SM"
              theme={isMicrophoneActiveFromHook ? "danger" : "primary"}
              text={isMicrophoneActiveFromHook ? "Disable" : "Enable"}
              onClick={handleToggleMicrophoneEnable}
              disabled={isLoading || isHttpsRequired}
            />
          </div>
          
          {/* HTTPS requirement notice */}
          {isHttpsRequired && (
            <div className="text-xs text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 p-2 rounded-md">
              <p className="font-medium mb-1">HTTPS Required for Microphone Input</p>
              <p>
                Microphone access requires a secure connection due to browser security policies. Audio output works fine on HTTP, but microphone input needs HTTPS.
              </p>
              <p className="mt-1">
                <span className="font-medium">Current:</span> {window.location.protocol + '//' + window.location.host}
                <br />
                <span className="font-medium">Secure:</span> {'https://' + window.location.host}
              </p>
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
               disabled={devicesLoading || isHttpsRequired}
              className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:bg-slate-50 disabled:text-slate-500 dark:border-slate-600 dark:bg-slate-700 dark:text-slate-300 dark:focus:border-blue-400 dark:disabled:bg-slate-800"
            >
              {audioInputDevices.map((device) => (
                <option key={device.deviceId} value={device.deviceId}>
                  {device.label}
                </option>
              ))}
            </select>
            {isHttpsRequired ? (
              <p className="text-xs text-amber-600 dark:text-amber-400">
                HTTPS connection required for microphone device selection
              </p>
            ) : isMicrophoneActiveFromHook ? (
               <p className="text-xs text-slate-500 dark:text-slate-400">
                 Changing device will restart the microphone
               </p>
             ) : null}
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

        {/* Audio Quality Info (fixed optimal configuration) */}
        {currentConfig && (
          <div className="space-y-2 rounded-md bg-slate-50 p-3 dark:bg-slate-800">
            <div className="flex items-center gap-2">
              <MdGraphicEq className="h-4 w-4 text-slate-600 dark:text-slate-400" />
              <span className="font-medium text-slate-900 dark:text-slate-100">
                Audio Configuration
              </span>
            </div>
            <div className="text-sm text-slate-600 dark:text-slate-400">
              Optimized for S16_LE @ 48kHz stereo HDMI audio
            </div>
            <div className="text-xs text-slate-500 dark:text-slate-500">
              Bitrate: {currentConfig.Bitrate} kbps | Sample Rate: {currentConfig.SampleRate} Hz | Channels: {currentConfig.Channels}
            </div>
          </div>
        )}



      </div>
    </div>
  );
}