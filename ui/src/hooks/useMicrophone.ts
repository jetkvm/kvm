import { useCallback, useEffect, useRef, useState } from "react";

import { useRTCStore, useSettingsStore } from "@/hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { useUsbDeviceConfig } from "@/hooks/useUsbDeviceConfig";
import { useAudioEvents, AudioDeviceChangedData } from "@/hooks/useAudioEvents";
import { devLog, devInfo, devWarn, devError, devOnly } from "@/utils/debug";
import { AUDIO_CONFIG } from "@/config/constants";

export interface MicrophoneError {
  type: 'permission' | 'device' | 'network' | 'unknown';
  message: string;
}

export function useMicrophone() {
  const {
    peerConnection,
    microphoneStream,
    setMicrophoneStream,
    microphoneSender,
    setMicrophoneSender,
    isMicrophoneActive,
    setMicrophoneActive,
    isMicrophoneMuted,
    setMicrophoneMuted,
    rpcDataChannel,
  } = useRTCStore();

  const { microphoneWasEnabled, setMicrophoneWasEnabled } = useSettingsStore();
  const { send } = useJsonRpc();

  // Check USB audio status and handle microphone restoration when USB audio is re-enabled
  const { usbDeviceConfig } = useUsbDeviceConfig();
  const isUsbAudioEnabled = usbDeviceConfig?.audio ?? true;
  
  // Track microphone state when USB audio gets disabled, so we can restore it when re-enabled
  const [microphoneWasActiveBeforeUsbDisable, setMicrophoneWasActiveBeforeUsbDisable] = useState<boolean>(false);

  // RPC helper functions to replace HTTP API calls
  const rpcMicrophoneStart = useCallback((): Promise<void> => {
    return new Promise((resolve, reject) => {
      if (rpcDataChannel?.readyState !== "open") {
        reject(new Error("Device connection not available"));
        return;
      }
      
      send("microphoneStart", {}, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          reject(new Error(resp.error.message));
        } else {
          resolve();
        }
      });
    });
  }, [rpcDataChannel?.readyState, send]);

  const microphoneStreamRef = useRef<MediaStream | null>(null);
  
  // Loading states
  const [isStarting, setIsStarting] = useState(false);
  const [isStopping, setIsStopping] = useState(false);
  const [isToggling, setIsToggling] = useState(false);

  // Add debouncing refs to prevent rapid operations
  const lastOperationRef = useRef<number>(0);
  const operationTimeoutRef = useRef<number | null>(null);

  // Debounced operation wrapper
  const debouncedOperation = useCallback((operation: () => Promise<void>, operationType: string) => {
    const now = Date.now();
    const timeSinceLastOp = now - lastOperationRef.current;
    
    if (timeSinceLastOp < AUDIO_CONFIG.OPERATION_DEBOUNCE_MS) {
      devLog(`Debouncing ${operationType} operation - too soon (${timeSinceLastOp}ms since last)`);
      return;
    }
    
    // Clear any pending operation
    if (operationTimeoutRef.current) {
      clearTimeout(operationTimeoutRef.current);
      operationTimeoutRef.current = null;
    }
    
    lastOperationRef.current = now;
    operation().catch(error => {
      devError(`Debounced ${operationType} operation failed:`, error);
    });
  }, []);

  // Cleanup function to stop microphone stream
  const stopMicrophoneStream = useCallback(async () => {
    if (microphoneStreamRef.current) {
      microphoneStreamRef.current.getTracks().forEach((track: MediaStreamTrack) => {
        track.stop();
      });
      microphoneStreamRef.current = null;
      setMicrophoneStream(null);
    }

    if (microphoneSender && peerConnection) {
      // Instead of removing the track, replace it with null to keep the transceiver
      try {
        await microphoneSender.replaceTrack(null);
      } catch (error) {
        devWarn("Failed to replace track with null:", error);
        // Fallback to removing the track
        peerConnection.removeTrack(microphoneSender);
      }
      setMicrophoneSender(null);
    }

    setMicrophoneActive(false);
    setMicrophoneMuted(false);
  }, [microphoneSender, peerConnection, setMicrophoneStream, setMicrophoneSender, setMicrophoneActive, setMicrophoneMuted]);



  const lastSyncRef = useRef<number>(0);
  const isStartingRef = useRef<boolean>(false); // Track if we're in the middle of starting
  
  const syncMicrophoneState = useCallback(async () => {
    // Debounce sync calls to prevent race conditions
    const now = Date.now();
    if (now - lastSyncRef.current < AUDIO_CONFIG.SYNC_DEBOUNCE_MS) {
      return;
    }
    lastSyncRef.current = now;
    
    // Don't sync if we're in the middle of starting the microphone
    if (isStartingRef.current) {
      return;
    }
    
    // Early return if RPC data channel is not ready
    if (rpcDataChannel?.readyState !== "open") {
      devWarn("RPC connection not available for microphone sync, skipping");
      return;
    }
    
    try {
      await new Promise<void>((resolve, reject) => {
        send("microphoneStatus", {}, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            devError("RPC microphone status failed:", resp.error);
            reject(new Error(resp.error.message));
          } else if ("result" in resp) {
            const data = resp.result as { running: boolean };
            const backendRunning = data.running;
            
            // Only sync if there's a significant state difference and we're not in a transition
            if (backendRunning !== isMicrophoneActive) {
              devInfo(`Syncing microphone state: backend=${backendRunning}, frontend=${isMicrophoneActive}`);
              
              // If backend is running but frontend thinks it's not, just update frontend state
              if (backendRunning && !isMicrophoneActive) {
                devLog("Backend running, updating frontend state to active");
                setMicrophoneActive(true);
              }
              // If backend is not running but frontend thinks it is, clean up and update state
              else if (!backendRunning && isMicrophoneActive) {
                devLog("Backend not running, cleaning up frontend state");
                setMicrophoneActive(false);
                // Only clean up stream if we actually have one
                if (microphoneStreamRef.current) {
                  stopMicrophoneStream();
                }
                setMicrophoneMuted(false);
              }
            }
            resolve();
          } else {
            reject(new Error("Invalid response"));
          }
        });
      });
    } catch (error) {
      devError("Error syncing microphone state:", error);
    }
  }, [isMicrophoneActive, setMicrophoneActive, setMicrophoneMuted, stopMicrophoneStream, rpcDataChannel?.readyState, send]);

  // Start microphone stream
  const startMicrophone = useCallback(async (deviceId?: string): Promise<{ success: boolean; error?: MicrophoneError }> => {
    // Prevent multiple simultaneous start operations
    if (isStarting || isStopping || isToggling) {
      devLog("Microphone operation already in progress, skipping start");
      return { success: false, error: { type: 'unknown', message: 'Operation already in progress' } };
    }
    
    setIsStarting(true);
    try {
      // Set flag to prevent sync during startup
      isStartingRef.current = true;
      // Request microphone permission and get stream
      const audioConstraints: MediaTrackConstraints = {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        sampleRate: AUDIO_CONFIG.SAMPLE_RATE,
        channelCount: AUDIO_CONFIG.CHANNEL_COUNT,
      };
      
      // Add device ID if specified
      if (deviceId && deviceId !== 'default') {
        audioConstraints.deviceId = { exact: deviceId };
      }
      
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints
      });

      // Store the stream in both ref and store
      microphoneStreamRef.current = stream;
      setMicrophoneStream(stream);

      // Add audio track to peer connection if available
      if (peerConnection && stream.getAudioTracks().length > 0) {
        const audioTrack = stream.getAudioTracks()[0];
        
        // Find the audio transceiver (should already exist with sendrecv direction)
        const transceivers = peerConnection.getTransceivers();
        
        // Look for an audio transceiver that can send (has sendrecv or sendonly direction)
        const audioTransceiver = transceivers.find((transceiver: RTCRtpTransceiver) => {
          // Check if this transceiver is for audio and can send
          const canSend = transceiver.direction === 'sendrecv' || transceiver.direction === 'sendonly';
          
          // For newly created transceivers, we need to check if they're for audio
          // We can do this by checking if the sender doesn't have a track yet and direction allows sending
          if (canSend && !transceiver.sender.track) {
            return true;
          }
          
          // For existing transceivers, check if they already have an audio track
          if (transceiver.sender.track?.kind === 'audio' || transceiver.receiver.track?.kind === 'audio') {
            return canSend;
          }
          
          return false;
        });

        let sender: RTCRtpSender;
        if (audioTransceiver && audioTransceiver.sender) {
          // Use the existing audio transceiver's sender
          await audioTransceiver.sender.replaceTrack(audioTrack);
          sender = audioTransceiver.sender;
        } else {
          // Fallback: add new track if no transceiver found
          sender = peerConnection.addTrack(audioTrack, stream);
        }
        
        setMicrophoneSender(sender);
        
        // Check sender stats to verify audio is being transmitted
        devOnly(() => {
          setTimeout(async () => {
            try {
              const stats = await sender.getStats();
              stats.forEach((report) => {
                if (report.type === 'outbound-rtp' && report.kind === 'audio') {
                  devLog("Audio RTP stats:", {
                    packetsSent: report.packetsSent,
                    bytesSent: report.bytesSent
                  });
                }
              });
            } catch (error) {
              devError("Failed to get sender stats:", error);
            }
          }, 2000);
        });
      }

      // Notify backend that microphone is started - only if USB audio is enabled
      if (!isUsbAudioEnabled) {
        devInfo("USB audio is disabled, skipping backend microphone start");
        // Still set frontend state as active since the stream was successfully created
        setMicrophoneActive(true);
        setMicrophoneMuted(false);
        setMicrophoneWasEnabled(true);
        isStartingRef.current = false;
        setIsStarting(false);
        return { success: true };
      }
      
      // Retry logic for backend failures
      let backendSuccess = false;
      let lastError: Error | string | null = null;
      
      for (let attempt = 1; attempt <= 3; attempt++) {
        // If this is a retry, first try to reset the backend microphone state
        if (attempt > 1) {
          try {
            // Use RPC for reset (cloud-compatible)
            if (rpcDataChannel?.readyState === "open") {
              await new Promise<void>((resolve) => {
                send("microphoneReset", {}, (resp: JsonRpcResponse) => {
                  if ("error" in resp) {
                    devWarn("RPC microphone reset failed:", resp.error);
                    // Try stop as fallback
                    send("microphoneStop", {}, (stopResp: JsonRpcResponse) => {
                      if ("error" in stopResp) {
                        devWarn("RPC microphone stop also failed:", stopResp.error);
                      }
                      resolve(); // Continue even if both fail
                    });
                  } else {
                    resolve();
                  }
                });
              });
              // Wait a bit for the backend to reset
              await new Promise(resolve => setTimeout(resolve, 200));
            } else {
              devWarn("RPC connection not available for reset");
            }
          } catch (resetError) {
            devWarn("Failed to reset backend state:", resetError);
          }
        }
        
        try {
          await rpcMicrophoneStart();
          backendSuccess = true;
          break; // Exit the retry loop on success
        } catch (rpcError) {
          lastError = `Backend RPC error: ${rpcError instanceof Error ? rpcError.message : 'Unknown error'}`;
          devError(`Backend microphone start failed with RPC error: ${lastError} (attempt ${attempt})`);
          
          // For RPC errors, try again after a short delay
          if (attempt < 3) {
            await new Promise(resolve => setTimeout(resolve, 500));
            continue;
          }
        }
      }
      
      // If all backend attempts failed, cleanup and return error
      if (!backendSuccess) {
        devError("All backend start attempts failed, cleaning up stream");
        await stopMicrophoneStream();
        isStartingRef.current = false;
        setIsStarting(false);
        return {
          success: false,
          error: {
            type: 'network',
            message: `Failed to start microphone on backend after 3 attempts. Last error: ${lastError}`
          }
        };
      }

      // Only set active state after backend confirms success
      setMicrophoneActive(true);
      setMicrophoneMuted(false);
      
      // Save microphone enabled state for auto-restore on page reload
      setMicrophoneWasEnabled(true);

      // Clear the starting flag
      isStartingRef.current = false;
      setIsStarting(false);
      return { success: true };
    } catch (error) {
      let micError: MicrophoneError;
      if (error instanceof Error) {
        if (error.name === 'NotAllowedError' || error.name === 'PermissionDeniedError') {
          micError = {
            type: 'permission',
            message: 'Microphone permission denied. Please allow microphone access and try again.'
          };
        } else if (error.name === 'NotFoundError' || error.name === 'DevicesNotFoundError') {
          micError = {
            type: 'device',
            message: 'No microphone device found. Please check your microphone connection.'
          };
        } else {
          micError = {
            type: 'unknown',
            message: error.message || 'Failed to access microphone'
          };
        }
      } else {
        micError = {
          type: 'unknown',
          message: 'Unknown error occurred while accessing microphone'
        };
      }

      // Clear the starting flag on error
      isStartingRef.current = false;
      setIsStarting(false);
      return { success: false, error: micError };
    }
  }, [peerConnection, setMicrophoneStream, setMicrophoneSender, setMicrophoneActive, setMicrophoneMuted, setMicrophoneWasEnabled, stopMicrophoneStream, isStarting, isStopping, isToggling, rpcMicrophoneStart, rpcDataChannel?.readyState, send, isUsbAudioEnabled]);



  // Stop microphone
  const stopMicrophone = useCallback(async (): Promise<{ success: boolean; error?: MicrophoneError }> => {
    // Prevent multiple simultaneous stop operations
    if (isStarting || isStopping || isToggling) {
      devLog("Microphone operation already in progress, skipping stop");
      return { success: false, error: { type: 'unknown', message: 'Operation already in progress' } };
    }
    
    setIsStopping(true);
    try {
      // First stop the stream
      await stopMicrophoneStream();

      // Then notify backend that microphone is stopped using RPC
      try {
        if (rpcDataChannel?.readyState === "open") {
          await new Promise<void>((resolve) => {
            send("microphoneStop", {}, (resp: JsonRpcResponse) => {
              if ("error" in resp) {
                devWarn("RPC microphone stop failed:", resp.error);
              }
              resolve(); // Continue regardless of result
            });
          });
        } else {
          devWarn("RPC connection not available for microphone stop");
        }
      } catch (error) {
        devWarn("Failed to notify backend about microphone stop:", error);
      }

      // Update frontend state immediately
      setMicrophoneActive(false);
      setMicrophoneMuted(false);
      
      // Save microphone disabled state for persistence
      setMicrophoneWasEnabled(false);

      // Sync state after stopping to ensure consistency (with longer delay)
      setTimeout(() => syncMicrophoneState(), 500);

      setIsStopping(false);
      return { success: true };
    } catch (error) {
      devError("Failed to stop microphone:", error);
      setIsStopping(false);
      return {
        success: false,
        error: {
          type: 'unknown',
          message: error instanceof Error ? error.message : 'Failed to stop microphone'
        }
      };
    }
  }, [stopMicrophoneStream, syncMicrophoneState, setMicrophoneActive, setMicrophoneMuted, setMicrophoneWasEnabled, isStarting, isStopping, isToggling, rpcDataChannel?.readyState, send]);

  // Toggle microphone mute
  const toggleMicrophoneMute = useCallback(async (): Promise<{ success: boolean; error?: MicrophoneError }> => {
    // Prevent multiple simultaneous toggle operations
    if (isStarting || isStopping || isToggling) {
      devLog("Microphone operation already in progress, skipping toggle");
      return { success: false, error: { type: 'unknown', message: 'Operation already in progress' } };
    }
    
    setIsToggling(true);
    try {
      // Use the ref instead of store value to avoid race conditions
      const currentStream = microphoneStreamRef.current || microphoneStream;
      
      if (!currentStream || !isMicrophoneActive) {
        const errorDetails = {
          hasStream: !!currentStream,
          isActive: isMicrophoneActive,
          streamId: currentStream?.id,
          audioTracks: currentStream?.getAudioTracks().length || 0
        };
        devWarn("Microphone mute failed: stream or active state missing", errorDetails);
        
        // Provide more specific error message
        let errorMessage = 'Microphone is not active';
        if (!currentStream) {
          errorMessage = 'No microphone stream found. Please restart the microphone.';
        } else if (!isMicrophoneActive) {
          errorMessage = 'Microphone is not marked as active. Please restart the microphone.';
        }
        
        setIsToggling(false);
        return {
          success: false,
          error: {
            type: 'device',
            message: errorMessage
          }
        };
      }

      const audioTracks = currentStream.getAudioTracks();
      if (audioTracks.length === 0) {
        setIsToggling(false);
        return {
          success: false,
          error: {
            type: 'device',
            message: 'No audio tracks found in microphone stream'
          }
        };
      }

      const newMutedState = !isMicrophoneMuted;
      
      // Mute/unmute the audio track
      audioTracks.forEach((track: MediaStreamTrack) => {
        track.enabled = !newMutedState;
      });

      setMicrophoneMuted(newMutedState);

      // Notify backend about mute state using RPC
      try {
        if (rpcDataChannel?.readyState === "open") {
          await new Promise<void>((resolve) => {
            send("microphoneMute", { muted: newMutedState }, (resp: JsonRpcResponse) => {
              if ("error" in resp) {
                devWarn("RPC microphone mute failed:", resp.error);
              }
              resolve(); // Continue regardless of result
            });
          });
        } else {
          devWarn("RPC connection not available for microphone mute");
        }
      } catch (error) {
        devWarn("Failed to notify backend about microphone mute:", error);
      }

      setIsToggling(false);
      return { success: true };
    } catch (error) {
      devError("Failed to toggle microphone mute:", error);
      setIsToggling(false);
      return {
        success: false,
        error: {
          type: 'unknown',
          message: error instanceof Error ? error.message : 'Failed to toggle microphone mute'
        }
      };
    }
  }, [microphoneStream, isMicrophoneActive, isMicrophoneMuted, setMicrophoneMuted, isStarting, isStopping, isToggling, rpcDataChannel?.readyState, send]);





  const startMicrophoneDebounced = useCallback((deviceId?: string) => {
    debouncedOperation(async () => {
      await startMicrophone(deviceId).catch(devError);
    }, "start");
  }, [startMicrophone, debouncedOperation]);

  const stopMicrophoneDebounced = useCallback(() => {
    debouncedOperation(async () => {
      await stopMicrophone().catch(devError);
    }, "stop");
  }, [stopMicrophone, debouncedOperation]);



  // Sync state on mount and auto-restore microphone if it was enabled before page reload
  useEffect(() => {
    const autoRestoreMicrophone = async () => {
      // Wait for RPC connection to be ready before attempting any operations
      if (rpcDataChannel?.readyState !== "open") {
        return;
      }
      
      // First sync the current state
      await syncMicrophoneState();
      
      // If microphone was enabled before page reload and is not currently active, restore it
      if (microphoneWasEnabled && !isMicrophoneActive && peerConnection) {
        try {
          const result = await startMicrophone();
          if (result.success) {
            devInfo("Microphone auto-restored successfully after page reload");
          } else {
            devWarn("Failed to auto-restore microphone:", result.error);
          }
        } catch (error) {
          devWarn("Error during microphone auto-restoration:", error);
        }
      }
    };
    
    // Add a delay to ensure RTC connection is fully established
    const timer = setTimeout(autoRestoreMicrophone, 1000);
    return () => clearTimeout(timer);
  }, [syncMicrophoneState, microphoneWasEnabled, isMicrophoneActive, peerConnection, startMicrophone, rpcDataChannel?.readyState]);

  // Handle audio device changes (USB audio enable/disable) via WebSocket events
  const handleAudioDeviceChanged = useCallback((data: AudioDeviceChangedData) => {
    devInfo("Audio device changed:", data);
    
    // USB audio was just disabled
    if (!data.enabled && data.reason === "usb_reconfiguration") {
      devInfo("USB audio disabled via device change event - storing microphone state");
      setMicrophoneWasActiveBeforeUsbDisable(isMicrophoneActive);
      // Clear the enabled flag to prevent auto-restore attempts
      if (isMicrophoneActive) {
        setMicrophoneWasEnabled(false);
      }
    }
    
    // USB audio was just re-enabled
    else if (data.enabled && data.reason === "usb_reconfiguration") {
      devInfo("USB audio re-enabled via device change event - checking if microphone should be restored");
      
      // If microphone was active before USB was disabled, restore it
      if (microphoneWasActiveBeforeUsbDisable && !isMicrophoneActive && rpcDataChannel?.readyState === "open") {
        devInfo("Restoring microphone after USB audio re-enabled");
        setTimeout(async () => {
          try {
            const result = await startMicrophone();
            if (result.success) {
              devInfo("Microphone successfully restored after USB audio re-enable");
              setMicrophoneWasEnabled(true);
            } else {
              devWarn("Failed to restore microphone after USB audio re-enable:", result.error);
            }
          } catch (error) {
            devWarn("Error restoring microphone after USB audio re-enable:", error);
          }
        }, 500); // Small delay to ensure USB device reconfiguration is complete
      }
      
      // Clear the stored state
      setMicrophoneWasActiveBeforeUsbDisable(false);
    }
  }, [isMicrophoneActive, microphoneWasActiveBeforeUsbDisable, startMicrophone, setMicrophoneWasEnabled, rpcDataChannel?.readyState]);

  // Subscribe to audio device change events
  useAudioEvents(handleAudioDeviceChanged);

  // Cleanup on unmount - use ref to avoid dependency on stopMicrophoneStream
  useEffect(() => {
    return () => {
      // Clean up stream directly without depending on the callback
      const stream = microphoneStreamRef.current;
      if (stream) {
        stream.getAudioTracks().forEach((track: MediaStreamTrack) => {
          track.stop();
        });
        microphoneStreamRef.current = null;
      }
    };
  }, []); // No dependencies to prevent re-running

  return {
    isMicrophoneActive,
    isMicrophoneMuted,
    microphoneStream,
    startMicrophone,
    stopMicrophone,
    toggleMicrophoneMute,

    // Expose debounced variants for UI handlers
    startMicrophoneDebounced,
    stopMicrophoneDebounced,
    // Expose sync and loading flags for consumers that expect them
    syncMicrophoneState,
    isStarting,
    isStopping,
    isToggling,
  };
}