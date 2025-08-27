import { useCallback, useEffect, useRef, useState } from "react";

import { useRTCStore } from "@/hooks/stores";
import api from "@/api";
import { devLog, devInfo, devWarn, devError, devOnly } from "@/utils/debug";
import { NETWORK_CONFIG, AUDIO_CONFIG } from "@/config/constants";

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
  } = useRTCStore();

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
    // Cleaning up microphone stream
    
    if (microphoneStreamRef.current) {
      microphoneStreamRef.current.getTracks().forEach(track => {
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

  // Debug function to check current state (can be called from browser console)
  const debugMicrophoneState = useCallback(() => {
    const refStream = microphoneStreamRef.current;
    const state = {
      isMicrophoneActive,
      isMicrophoneMuted,
      streamInRef: !!refStream,
      streamInStore: !!microphoneStream,
      senderInStore: !!microphoneSender,
      streamId: refStream?.id,
      storeStreamId: microphoneStream?.id,
      audioTracks: refStream?.getAudioTracks().length || 0,
      storeAudioTracks: microphoneStream?.getAudioTracks().length || 0,
      audioTrackDetails: refStream?.getAudioTracks().map(track => ({
        id: track.id,
        label: track.label,
        enabled: track.enabled,
        readyState: track.readyState,
        muted: track.muted
      })) || [],
      peerConnectionState: peerConnection ? {
        connectionState: peerConnection.connectionState,
        iceConnectionState: peerConnection.iceConnectionState,
        signalingState: peerConnection.signalingState
      } : "No peer connection",
      streamMatch: refStream === microphoneStream
    };
    devLog("Microphone Debug State:", state);
    
    // Also check if streams are active
    if (refStream) {
      devLog("Ref stream active tracks:", refStream.getAudioTracks().filter(t => t.readyState === 'live').length);
    }
    if (microphoneStream && microphoneStream !== refStream) {
      devLog("Store stream active tracks:", microphoneStream.getAudioTracks().filter(t => t.readyState === 'live').length);
    }
    
    return state;
  }, [isMicrophoneActive, isMicrophoneMuted, microphoneStream, microphoneSender, peerConnection]);

  // Make debug function available globally for console access
  useEffect(() => {
    (window as Window & { debugMicrophoneState?: () => unknown }).debugMicrophoneState = debugMicrophoneState;
    return () => {
      delete (window as Window & { debugMicrophoneState?: () => unknown }).debugMicrophoneState;
    };
  }, [debugMicrophoneState]);

  const lastSyncRef = useRef<number>(0);
  const isStartingRef = useRef<boolean>(false); // Track if we're in the middle of starting
  
  const syncMicrophoneState = useCallback(async () => {
    // Debounce sync calls to prevent race conditions
    const now = Date.now();
    if (now - lastSyncRef.current < AUDIO_CONFIG.SYNC_DEBOUNCE_MS) {
      devLog("Skipping sync - too frequent");
      return;
    }
    lastSyncRef.current = now;
    
    // Don't sync if we're in the middle of starting the microphone
    if (isStartingRef.current) {
      devLog("Skipping sync - microphone is starting");
      return;
    }
    
    try {
      const response = await api.GET("/microphone/status", {});
      if (response.ok) {
        const data = await response.json();
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
              devLog("Cleaning up orphaned stream");
              await stopMicrophoneStream();
            }
          }
        }
      }
    } catch (error) {
      devWarn("Failed to sync microphone state:", error);
    }
  }, [isMicrophoneActive, setMicrophoneActive, stopMicrophoneStream]);

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
      
      devLog("Requesting microphone with constraints:", audioConstraints);
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints
      });

      // Microphone stream created successfully

      // Store the stream in both ref and store
      microphoneStreamRef.current = stream;
      setMicrophoneStream(stream);
      
      // Verify the stream was stored correctly
      devLog("Stream storage verification:", {
        refSet: !!microphoneStreamRef.current,
        refId: microphoneStreamRef.current?.id,
        storeWillBeSet: true // Store update is async
      });

      // Add audio track to peer connection if available
      devLog("Peer connection state:", peerConnection ? {
        connectionState: peerConnection.connectionState,
        iceConnectionState: peerConnection.iceConnectionState,
        signalingState: peerConnection.signalingState
      } : "No peer connection");
      
      if (peerConnection && stream.getAudioTracks().length > 0) {
        const audioTrack = stream.getAudioTracks()[0];
        devLog("Starting microphone with audio track:", audioTrack.id, "kind:", audioTrack.kind);
        
        // Find the audio transceiver (should already exist with sendrecv direction)
        const transceivers = peerConnection.getTransceivers();
        devLog("Available transceivers:", transceivers.map(t => ({
          direction: t.direction,
          mid: t.mid,
          senderTrack: t.sender.track?.kind,
          receiverTrack: t.receiver.track?.kind
        })));
        
        // Look for an audio transceiver that can send (has sendrecv or sendonly direction)
        const audioTransceiver = transceivers.find(transceiver => {
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

        devLog("Found audio transceiver:", audioTransceiver ? {
          direction: audioTransceiver.direction,
          mid: audioTransceiver.mid,
          senderTrack: audioTransceiver.sender.track?.kind,
          receiverTrack: audioTransceiver.receiver.track?.kind
        } : null);

        let sender: RTCRtpSender;
        if (audioTransceiver && audioTransceiver.sender) {
          // Use the existing audio transceiver's sender
          await audioTransceiver.sender.replaceTrack(audioTrack);
          sender = audioTransceiver.sender;
          devLog("Replaced audio track on existing transceiver");
          
          // Verify the track was set correctly
          devLog("Transceiver after track replacement:", {
            direction: audioTransceiver.direction,
            senderTrack: audioTransceiver.sender.track?.id,
            senderTrackKind: audioTransceiver.sender.track?.kind,
            senderTrackEnabled: audioTransceiver.sender.track?.enabled,
            senderTrackReadyState: audioTransceiver.sender.track?.readyState
          });
        } else {
          // Fallback: add new track if no transceiver found
          sender = peerConnection.addTrack(audioTrack, stream);
          devLog("Added new audio track to peer connection");
          
          // Find the transceiver that was created for this track
          const newTransceiver = peerConnection.getTransceivers().find(t => t.sender === sender);
          devLog("New transceiver created:", newTransceiver ? {
            direction: newTransceiver.direction,
            senderTrack: newTransceiver.sender.track?.id,
            senderTrackKind: newTransceiver.sender.track?.kind
          } : "Not found");
        }
        
        setMicrophoneSender(sender);
        devLog("Microphone sender set:", {
          senderId: sender,
          track: sender.track?.id,
          trackKind: sender.track?.kind,
          trackEnabled: sender.track?.enabled,
          trackReadyState: sender.track?.readyState
        });
        
        // Check sender stats to verify audio is being transmitted
        devOnly(() => {
          setTimeout(async () => {
            try {
              const stats = await sender.getStats();
              devLog("Sender stats after 2 seconds:");
              stats.forEach((report, id) => {
                if (report.type === 'outbound-rtp' && report.kind === 'audio') {
                  devLog("Outbound audio RTP stats:", {
                    id,
                    packetsSent: report.packetsSent,
                    bytesSent: report.bytesSent,
                    timestamp: report.timestamp
                  });
                }
              });
            } catch (error) {
              devError("Failed to get sender stats:", error);
            }
          }, 2000);
        });
      }

      // Notify backend that microphone is started
      devLog("Notifying backend about microphone start...");
      
      // Retry logic for backend failures
      let backendSuccess = false;
      let lastError: Error | string | null = null;
      
      for (let attempt = 1; attempt <= 3; attempt++) {
        try {
          // If this is a retry, first try to reset the backend microphone state
          if (attempt > 1) {
            devLog(`Backend start attempt ${attempt}, first trying to reset backend state...`);
            try {
              // Try the new reset endpoint first
              const resetResp = await api.POST("/microphone/reset", {});
              if (resetResp.ok) {
                devLog("Backend reset successful");
              } else {
                // Fallback to stop
                await api.POST("/microphone/stop", {});
              }
              // Wait a bit for the backend to reset
              await new Promise(resolve => setTimeout(resolve, 200));
            } catch (resetError) {
              devWarn("Failed to reset backend state:", resetError);
            }
          }
          
          const backendResp = await api.POST("/microphone/start", {});
          devLog(`Backend response status (attempt ${attempt}):`, backendResp.status, "ok:", backendResp.ok);
          
          if (!backendResp.ok) {
            lastError = `Backend returned status ${backendResp.status}`;
            devError(`Backend microphone start failed with status: ${backendResp.status} (attempt ${attempt})`);
            
            // For 500 errors, try again after a short delay
            if (backendResp.status === 500 && attempt < 3) {
              devLog(`Retrying backend start in 500ms (attempt ${attempt + 1}/3)...`);
              await new Promise(resolve => setTimeout(resolve, 500));
              continue;
            }
          } else {
            // Success!
            const responseData = await backendResp.json();
            devLog("Backend response data:", responseData);
            if (responseData.status === "already running") {
              devInfo("Backend microphone was already running");
              
              // If we're on the first attempt and backend says "already running",
              // but frontend thinks it's not active, this might be a stuck state
              if (attempt === 1 && !isMicrophoneActive) {
                devWarn("Backend reports 'already running' but frontend is not active - possible stuck state");
                devLog("Attempting to reset backend state and retry...");
                
                try {
                  const resetResp = await api.POST("/microphone/reset", {});
                  if (resetResp.ok) {
                    devLog("Backend reset successful, retrying start...");
                    await new Promise(resolve => setTimeout(resolve, 200));
                    continue; // Retry the start
                  }
                } catch (resetError) {
                  devWarn("Failed to reset stuck backend state:", resetError);
                }
              }
            }
            devLog("Backend microphone start successful");
            backendSuccess = true;
            break;
          }
        } catch (error) {
          lastError = error instanceof Error ? error : String(error);
          devError(`Backend microphone start threw error (attempt ${attempt}):`, error);
          
          // For network errors, try again after a short delay
          if (attempt < 3) {
            devLog(`Retrying backend start in 500ms (attempt ${attempt + 1}/3)...`);
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
      
      devLog("Microphone state set to active. Verifying state:", {
        streamInRef: !!microphoneStreamRef.current,
        streamInStore: !!microphoneStream,
        isActive: true,
        isMuted: false
      });

      // Don't sync immediately after starting - it causes race conditions
      // The sync will happen naturally through other triggers
      devOnly(() => {
        setTimeout(() => {
          // Just verify state after a delay for debugging
          devLog("State check after delay:", {
            streamInRef: !!microphoneStreamRef.current,
            streamInStore: !!microphoneStream,
            isActive: isMicrophoneActive,
            isMuted: isMicrophoneMuted
          });
        }, AUDIO_CONFIG.AUDIO_TEST_TIMEOUT);
      });

      // Clear the starting flag
      isStartingRef.current = false;
      setIsStarting(false);
      return { success: true };
    } catch (error) {
      // Failed to start microphone
      
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
  }, [peerConnection, setMicrophoneStream, setMicrophoneSender, setMicrophoneActive, setMicrophoneMuted, stopMicrophoneStream, isMicrophoneActive, isMicrophoneMuted, microphoneStream, isStarting, isStopping, isToggling]);

  // Reset backend microphone state
  const resetBackendMicrophoneState = useCallback(async (): Promise<boolean> => {
    try {
      devLog("Resetting backend microphone state...");
      const response = await api.POST("/microphone/reset", {});
      
      if (response.ok) {
        const data = await response.json();
        devLog("Backend microphone reset successful:", data);
        
        // Update frontend state to match backend
        setMicrophoneActive(false);
        setMicrophoneMuted(false);
        
        // Clean up any orphaned streams
        if (microphoneStreamRef.current) {
          devLog("Cleaning up orphaned stream after reset");
          await stopMicrophoneStream();
        }
        
        // Wait a bit for everything to settle
        await new Promise(resolve => setTimeout(resolve, 200));
        
        // Sync state to ensure consistency
        await syncMicrophoneState();
        
        return true;
      } else {
        devError("Backend microphone reset failed:", response.status);
        return false;
      }
    } catch (error) {
      devWarn("Failed to reset backend microphone state:", error);
      // Fallback to old method
      try {
        devLog("Trying fallback reset method...");
        await api.POST("/microphone/stop", {});
        await new Promise(resolve => setTimeout(resolve, 300));
        return true;
      } catch (fallbackError) {
        devError("Fallback reset also failed:", fallbackError);
        return false;
      }
    }
  }, [setMicrophoneActive, setMicrophoneMuted, stopMicrophoneStream, syncMicrophoneState]);

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

      // Then notify backend that microphone is stopped
      try {
        await api.POST("/microphone/stop", {});
        devLog("Backend notified about microphone stop");
      } catch (error) {
        devWarn("Failed to notify backend about microphone stop:", error);
      }

      // Update frontend state immediately
      setMicrophoneActive(false);
      setMicrophoneMuted(false);

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
  }, [stopMicrophoneStream, syncMicrophoneState, setMicrophoneActive, setMicrophoneMuted, isStarting, isStopping, isToggling]);

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
      
      devLog("Toggle microphone mute - current state:", {
        hasRefStream: !!microphoneStreamRef.current,
        hasStoreStream: !!microphoneStream,
        isActive: isMicrophoneActive,
        isMuted: isMicrophoneMuted,
        streamId: currentStream?.id,
        audioTracks: currentStream?.getAudioTracks().length || 0
      });
      
      if (!currentStream || !isMicrophoneActive) {
        const errorDetails = {
          hasStream: !!currentStream,
          isActive: isMicrophoneActive,
          storeStream: !!microphoneStream,
          refStream: !!microphoneStreamRef.current,
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
      audioTracks.forEach(track => {
        track.enabled = !newMutedState;
        devLog(`Audio track ${track.id} enabled: ${track.enabled}`);
      });

      setMicrophoneMuted(newMutedState);

      // Notify backend about mute state
      try {
        await api.POST("/microphone/mute", { muted: newMutedState });
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
  }, [microphoneStream, isMicrophoneActive, isMicrophoneMuted, setMicrophoneMuted, isStarting, isStopping, isToggling]);

  // Function to check WebRTC audio transmission stats
  const checkAudioTransmissionStats = useCallback(async () => {
    if (!microphoneSender) {
      devLog("No microphone sender available");
      return null;
    }

    try {
      const stats = await microphoneSender.getStats();
      const audioStats: {
        id: string;
        type: string;
        kind: string;
        packetsSent?: number;
        bytesSent?: number;
        timestamp?: number;
        ssrc?: number;
      }[] = [];
      
      stats.forEach((report, id) => {
        if (report.type === 'outbound-rtp' && report.kind === 'audio') {
          audioStats.push({
            id,
            type: report.type,
            kind: report.kind,
            packetsSent: report.packetsSent,
            bytesSent: report.bytesSent,
            timestamp: report.timestamp,
            ssrc: report.ssrc
          });
        }
      });
      
      devLog("Audio transmission stats:", audioStats);
      return audioStats;
    } catch (error) {
      devError("Failed to get audio transmission stats:", error);
      return null;
    }
  }, [microphoneSender]);

  // Comprehensive test function to diagnose microphone issues
  const testMicrophoneAudio = useCallback(async () => {
    devLog("=== MICROPHONE AUDIO TEST ===");
    
    // 1. Check if we have a stream
    const stream = microphoneStreamRef.current;
    if (!stream) {
      devLog("❌ No microphone stream available");
      return;
    }
    
    devLog("✅ Microphone stream exists:", stream.id);
    
    // 2. Check audio tracks
    const audioTracks = stream.getAudioTracks();
    devLog("Audio tracks:", audioTracks.length);
    
    if (audioTracks.length === 0) {
      devLog("❌ No audio tracks in stream");
      return;
    }
    
    const track = audioTracks[0];
    devLog("✅ Audio track details:", {
      id: track.id,
      label: track.label,
      enabled: track.enabled,
      readyState: track.readyState,
      muted: track.muted
    });
    
    // 3. Test audio level detection manually
    try {
      const audioContext = new (window.AudioContext || (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext)();
      const analyser = audioContext.createAnalyser();
      const source = audioContext.createMediaStreamSource(stream);
      
      analyser.fftSize = AUDIO_CONFIG.ANALYSIS_FFT_SIZE;
      source.connect(analyser);
      
      const dataArray = new Uint8Array(analyser.frequencyBinCount);
      
      devLog("🎤 Testing audio level detection for 5 seconds...");
      devLog("Please speak into your microphone now!");
      
      let maxLevel = 0;
      let sampleCount = 0;
      
      const testInterval = setInterval(() => {
        analyser.getByteFrequencyData(dataArray);
        
        let sum = 0;
        for (const value of dataArray) {
          sum += value * value;
        }
        const rms = Math.sqrt(sum / dataArray.length);
        const level = Math.min(AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE, (rms / AUDIO_CONFIG.LEVEL_SCALING_FACTOR) * AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE);
        
        maxLevel = Math.max(maxLevel, level);
        sampleCount++;
        
        if (sampleCount % 10 === 0) { // Log every 10th sample
          devLog(`Audio level: ${level.toFixed(1)}% (max so far: ${maxLevel.toFixed(1)}%)`);
        }
      }, AUDIO_CONFIG.ANALYSIS_UPDATE_INTERVAL);
      
      setTimeout(() => {
        clearInterval(testInterval);
        source.disconnect();
        audioContext.close();
        
        devLog("🎤 Audio test completed!");
        devLog(`Maximum audio level detected: ${maxLevel.toFixed(1)}%`);
        
        if (maxLevel > 5) {
          devLog("✅ Microphone is detecting audio!");
        } else {
          devLog("❌ No significant audio detected. Check microphone permissions and hardware.");
        }
      }, NETWORK_CONFIG.AUDIO_TEST_DURATION);
      
    } catch (error) {
      devError("❌ Failed to test audio level:", error);
    }
    
    // 4. Check WebRTC sender
    if (microphoneSender) {
      devLog("✅ WebRTC sender exists");
      devLog("Sender track:", {
        id: microphoneSender.track?.id,
        kind: microphoneSender.track?.kind,
        enabled: microphoneSender.track?.enabled,
        readyState: microphoneSender.track?.readyState
      });
      
      // Check if sender track matches stream track
      if (microphoneSender.track === track) {
        devLog("✅ Sender track matches stream track");
      } else {
        devLog("❌ Sender track does NOT match stream track");
      }
    } else {
      devLog("❌ No WebRTC sender available");
    }
    
    // 5. Check peer connection
    if (peerConnection) {
      devLog("✅ Peer connection exists");
      devLog("Connection state:", peerConnection.connectionState);
      devLog("ICE connection state:", peerConnection.iceConnectionState);
      
      const transceivers = peerConnection.getTransceivers();
      const audioTransceivers = transceivers.filter(t => 
        t.sender.track?.kind === 'audio' || t.receiver.track?.kind === 'audio'
      );
      
      devLog("Audio transceivers:", audioTransceivers.map(t => ({
        direction: t.direction,
        senderTrack: t.sender.track?.id,
        receiverTrack: t.receiver.track?.id
      })));
    } else {
      devLog("❌ No peer connection available");
    }
    
  }, [microphoneSender, peerConnection]);

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

  // Make debug functions available globally for console access
  useEffect(() => {
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
      resetBackendMicrophone?: () => unknown;
    }).debugMicrophone = debugMicrophoneState;
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
      resetBackendMicrophone?: () => unknown;
    }).checkAudioStats = checkAudioTransmissionStats;
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
      resetBackendMicrophone?: () => unknown;
    }).testMicrophoneAudio = testMicrophoneAudio;
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
      resetBackendMicrophone?: () => unknown;
    }).resetBackendMicrophone = resetBackendMicrophoneState;
    return () => {
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
        resetBackendMicrophone?: () => unknown;
      }).debugMicrophone;
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
        resetBackendMicrophone?: () => unknown;
      }).checkAudioStats;
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
        resetBackendMicrophone?: () => unknown;
      }).testMicrophoneAudio;
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
        resetBackendMicrophone?: () => unknown;
      }).resetBackendMicrophone;
    };
  }, [debugMicrophoneState, checkAudioTransmissionStats, testMicrophoneAudio, resetBackendMicrophoneState]);

  // Sync state on mount
  useEffect(() => {
    syncMicrophoneState();
  }, [syncMicrophoneState]);

  // Cleanup on unmount - use ref to avoid dependency on stopMicrophoneStream
  useEffect(() => {
    return () => {
      // Clean up stream directly without depending on the callback
      const stream = microphoneStreamRef.current;
      if (stream) {
        devLog("Cleanup: stopping microphone stream on unmount");
        stream.getAudioTracks().forEach(track => {
          track.stop();
          devLog(`Cleanup: stopped audio track ${track.id}`);
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
    debugMicrophoneState,
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