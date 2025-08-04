import { useCallback, useEffect, useRef } from "react";

import { useRTCStore } from "@/hooks/stores";
import api from "@/api";

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

  // Cleanup function to stop microphone stream
  const stopMicrophoneStream = useCallback(async () => {
    console.log("stopMicrophoneStream called - cleaning up stream");
    console.trace("stopMicrophoneStream call stack");
    
    if (microphoneStreamRef.current) {
      console.log("Stopping microphone stream:", microphoneStreamRef.current.id);
      microphoneStreamRef.current.getTracks().forEach(track => {
        track.stop();
      });
      microphoneStreamRef.current = null;
      setMicrophoneStream(null);
      console.log("Microphone stream cleared from ref and store");
    } else {
      console.log("No microphone stream to stop");
    }

    if (microphoneSender && peerConnection) {
      // Instead of removing the track, replace it with null to keep the transceiver
      try {
        await microphoneSender.replaceTrack(null);
      } catch (error) {
        console.warn("Failed to replace track with null:", error);
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
    console.log("Microphone Debug State:", state);
    
    // Also check if streams are active
    if (refStream) {
      console.log("Ref stream active tracks:", refStream.getAudioTracks().filter(t => t.readyState === 'live').length);
    }
    if (microphoneStream && microphoneStream !== refStream) {
      console.log("Store stream active tracks:", microphoneStream.getAudioTracks().filter(t => t.readyState === 'live').length);
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
    if (now - lastSyncRef.current < 500) {
      console.log("Skipping sync - too frequent");
      return;
    }
    lastSyncRef.current = now;
    
    // Don't sync if we're in the middle of starting the microphone
    if (isStartingRef.current) {
      console.log("Skipping sync - microphone is starting");
      return;
    }
    
    try {
      const response = await api.GET("/microphone/status", {});
      if (response.ok) {
        const data = await response.json();
        const backendRunning = data.running;
        
        // If backend state differs from frontend state, sync them
        if (backendRunning !== isMicrophoneActive) {
          console.info(`Syncing microphone state: backend=${backendRunning}, frontend=${isMicrophoneActive}`);
          setMicrophoneActive(backendRunning);
          
          // Only clean up stream if backend is definitely not running AND we have a stream
          // Use ref to get current stream state, not stale closure value
          if (!backendRunning && microphoneStreamRef.current) {
            console.log("Backend not running, cleaning up stream");
            await stopMicrophoneStream();
          }
        }
      }
    } catch (error) {
      console.warn("Failed to sync microphone state:", error);
    }
  }, [isMicrophoneActive, setMicrophoneActive, stopMicrophoneStream]);

  // Start microphone stream
  const startMicrophone = useCallback(async (deviceId?: string): Promise<{ success: boolean; error?: MicrophoneError }> => {
    try {
      // Set flag to prevent sync during startup
      isStartingRef.current = true;
      // Request microphone permission and get stream
      const audioConstraints: MediaTrackConstraints = {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        sampleRate: 48000,
        channelCount: 1,
      };
      
      // Add device ID if specified
      if (deviceId && deviceId !== 'default') {
        audioConstraints.deviceId = { exact: deviceId };
      }
      
      console.log("Requesting microphone with constraints:", audioConstraints);
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints
      });

      console.log("Microphone stream created successfully:", {
        streamId: stream.id,
        audioTracks: stream.getAudioTracks().length,
        videoTracks: stream.getVideoTracks().length,
        audioTrackDetails: stream.getAudioTracks().map(track => ({
          id: track.id,
          label: track.label,
          enabled: track.enabled,
          readyState: track.readyState
        }))
      });

      // Store the stream in both ref and store
      microphoneStreamRef.current = stream;
      setMicrophoneStream(stream);
      
      // Verify the stream was stored correctly
      console.log("Stream storage verification:", {
        refSet: !!microphoneStreamRef.current,
        refId: microphoneStreamRef.current?.id,
        storeWillBeSet: true // Store update is async
      });

      // Add audio track to peer connection if available
      console.log("Peer connection state:", peerConnection ? {
        connectionState: peerConnection.connectionState,
        iceConnectionState: peerConnection.iceConnectionState,
        signalingState: peerConnection.signalingState
      } : "No peer connection");
      
      if (peerConnection && stream.getAudioTracks().length > 0) {
        const audioTrack = stream.getAudioTracks()[0];
        console.log("Starting microphone with audio track:", audioTrack.id, "kind:", audioTrack.kind);
        
        // Find the audio transceiver (should already exist with sendrecv direction)
        const transceivers = peerConnection.getTransceivers();
        console.log("Available transceivers:", transceivers.map(t => ({
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

        console.log("Found audio transceiver:", audioTransceiver ? {
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
          console.log("Replaced audio track on existing transceiver");
          
          // Verify the track was set correctly
          console.log("Transceiver after track replacement:", {
            direction: audioTransceiver.direction,
            senderTrack: audioTransceiver.sender.track?.id,
            senderTrackKind: audioTransceiver.sender.track?.kind,
            senderTrackEnabled: audioTransceiver.sender.track?.enabled,
            senderTrackReadyState: audioTransceiver.sender.track?.readyState
          });
        } else {
          // Fallback: add new track if no transceiver found
          sender = peerConnection.addTrack(audioTrack, stream);
          console.log("Added new audio track to peer connection");
          
          // Find the transceiver that was created for this track
          const newTransceiver = peerConnection.getTransceivers().find(t => t.sender === sender);
          console.log("New transceiver created:", newTransceiver ? {
            direction: newTransceiver.direction,
            senderTrack: newTransceiver.sender.track?.id,
            senderTrackKind: newTransceiver.sender.track?.kind
          } : "Not found");
        }
        
        setMicrophoneSender(sender);
        console.log("Microphone sender set:", {
          senderId: sender,
          track: sender.track?.id,
          trackKind: sender.track?.kind,
          trackEnabled: sender.track?.enabled,
          trackReadyState: sender.track?.readyState
        });
        
        // Check sender stats to verify audio is being transmitted
        setTimeout(async () => {
          try {
            const stats = await sender.getStats();
            console.log("Sender stats after 2 seconds:");
            stats.forEach((report, id) => {
              if (report.type === 'outbound-rtp' && report.kind === 'audio') {
                console.log("Outbound audio RTP stats:", {
                  id,
                  packetsSent: report.packetsSent,
                  bytesSent: report.bytesSent,
                  timestamp: report.timestamp
                });
              }
            });
          } catch (error) {
            console.error("Failed to get sender stats:", error);
          }
        }, 2000);
      }

      // Notify backend that microphone is started
      console.log("Notifying backend about microphone start...");
      try {
        const backendResp = await api.POST("/microphone/start", {});
        console.log("Backend response status:", backendResp.status, "ok:", backendResp.ok);
        
        if (!backendResp.ok) {
          console.error("Backend microphone start failed with status:", backendResp.status);
        // If backend fails, cleanup the stream
        await stopMicrophoneStream();
        isStartingRef.current = false;
        return {
          success: false,
          error: {
            type: 'network',
            message: 'Failed to start microphone on backend'
          }
        };
        }
        
        // Check the response to see if it was already running
        const responseData = await backendResp.json();
        console.log("Backend response data:", responseData);
        if (responseData.status === "already running") {
          console.info("Backend microphone was already running");
        }
        console.log("Backend microphone start successful");
      } catch (error) {
        console.error("Backend microphone start threw error:", error);
        // If backend fails, cleanup the stream
        await stopMicrophoneStream();
        isStartingRef.current = false;
        return {
          success: false,
          error: {
            type: 'network',
            message: 'Failed to communicate with backend'
          }
        };
      }

      // Only set active state after backend confirms success
      setMicrophoneActive(true);
      setMicrophoneMuted(false);
      
      console.log("Microphone state set to active. Verifying state:", {
        streamInRef: !!microphoneStreamRef.current,
        streamInStore: !!microphoneStream,
        isActive: true,
        isMuted: false
      });

      // Don't sync immediately after starting - it causes race conditions
      // The sync will happen naturally through other triggers
      setTimeout(() => {
        // Just verify state after a delay for debugging
        console.log("State check after delay:", {
          streamInRef: !!microphoneStreamRef.current,
          streamInStore: !!microphoneStream,
          isActive: isMicrophoneActive,
          isMuted: isMicrophoneMuted
        });
      }, 100);

      // Clear the starting flag
      isStartingRef.current = false;
      return { success: true };
    } catch (error) {
      console.error("Failed to start microphone:", error);
      
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
      return { success: false, error: micError };
    }
  }, [peerConnection, setMicrophoneStream, setMicrophoneSender, setMicrophoneActive, setMicrophoneMuted, stopMicrophoneStream, isMicrophoneActive, isMicrophoneMuted, microphoneStream]);

  // Stop microphone
  const stopMicrophone = useCallback(async (): Promise<{ success: boolean; error?: MicrophoneError }> => {
    try {
      await stopMicrophoneStream();

      // Notify backend that microphone is stopped
      try {
        await api.POST("/microphone/stop", {});
      } catch (error) {
        console.warn("Failed to notify backend about microphone stop:", error);
      }

      // Sync state after stopping to ensure consistency
      setTimeout(() => syncMicrophoneState(), 100);

      return { success: true };
    } catch (error) {
      console.error("Failed to stop microphone:", error);
      return {
        success: false,
        error: {
          type: 'unknown',
          message: error instanceof Error ? error.message : 'Failed to stop microphone'
        }
      };
    }
  }, [stopMicrophoneStream, syncMicrophoneState]);

  // Toggle microphone mute
  const toggleMicrophoneMute = useCallback(async (): Promise<{ success: boolean; error?: MicrophoneError }> => {
    try {
      // Use the ref instead of store value to avoid race conditions
      const currentStream = microphoneStreamRef.current || microphoneStream;
      
      console.log("Toggle microphone mute - current state:", {
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
        console.warn("Microphone mute failed: stream or active state missing", errorDetails);
        
        // Provide more specific error message
        let errorMessage = 'Microphone is not active';
        if (!currentStream) {
          errorMessage = 'No microphone stream found. Please restart the microphone.';
        } else if (!isMicrophoneActive) {
          errorMessage = 'Microphone is not marked as active. Please restart the microphone.';
        }
        
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
        console.log(`Audio track ${track.id} enabled: ${track.enabled}`);
      });

      setMicrophoneMuted(newMutedState);

      // Notify backend about mute state
      try {
        await api.POST("/microphone/mute", { muted: newMutedState });
      } catch (error) {
        console.warn("Failed to notify backend about microphone mute:", error);
      }

      return { success: true };
    } catch (error) {
      console.error("Failed to toggle microphone mute:", error);
      return {
        success: false,
        error: {
          type: 'unknown',
          message: error instanceof Error ? error.message : 'Failed to toggle microphone mute'
        }
      };
    }
  }, [microphoneStream, isMicrophoneActive, isMicrophoneMuted, setMicrophoneMuted]);

  // Function to check WebRTC audio transmission stats
  const checkAudioTransmissionStats = useCallback(async () => {
    if (!microphoneSender) {
      console.log("No microphone sender available");
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
      
      console.log("Audio transmission stats:", audioStats);
      return audioStats;
    } catch (error) {
      console.error("Failed to get audio transmission stats:", error);
      return null;
    }
  }, [microphoneSender]);

  // Comprehensive test function to diagnose microphone issues
  const testMicrophoneAudio = useCallback(async () => {
    console.log("=== MICROPHONE AUDIO TEST ===");
    
    // 1. Check if we have a stream
    const stream = microphoneStreamRef.current;
    if (!stream) {
      console.log("❌ No microphone stream available");
      return;
    }
    
    console.log("✅ Microphone stream exists:", stream.id);
    
    // 2. Check audio tracks
    const audioTracks = stream.getAudioTracks();
    console.log("Audio tracks:", audioTracks.length);
    
    if (audioTracks.length === 0) {
      console.log("❌ No audio tracks in stream");
      return;
    }
    
    const track = audioTracks[0];
    console.log("✅ Audio track details:", {
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
      
      analyser.fftSize = 256;
      source.connect(analyser);
      
      const dataArray = new Uint8Array(analyser.frequencyBinCount);
      
      console.log("🎤 Testing audio level detection for 5 seconds...");
      console.log("Please speak into your microphone now!");
      
      let maxLevel = 0;
      let sampleCount = 0;
      
      const testInterval = setInterval(() => {
        analyser.getByteFrequencyData(dataArray);
        
        let sum = 0;
        for (const value of dataArray) {
          sum += value * value;
        }
        const rms = Math.sqrt(sum / dataArray.length);
        const level = Math.min(100, (rms / 255) * 100);
        
        maxLevel = Math.max(maxLevel, level);
        sampleCount++;
        
        if (sampleCount % 10 === 0) { // Log every 10th sample
          console.log(`Audio level: ${level.toFixed(1)}% (max so far: ${maxLevel.toFixed(1)}%)`);
        }
      }, 100);
      
      setTimeout(() => {
        clearInterval(testInterval);
        source.disconnect();
        audioContext.close();
        
        console.log("🎤 Audio test completed!");
        console.log(`Maximum audio level detected: ${maxLevel.toFixed(1)}%`);
        
        if (maxLevel > 5) {
          console.log("✅ Microphone is detecting audio!");
        } else {
          console.log("❌ No significant audio detected. Check microphone permissions and hardware.");
        }
      }, 5000);
      
    } catch (error) {
      console.error("❌ Failed to test audio level:", error);
    }
    
    // 4. Check WebRTC sender
    if (microphoneSender) {
      console.log("✅ WebRTC sender exists");
      console.log("Sender track:", {
        id: microphoneSender.track?.id,
        kind: microphoneSender.track?.kind,
        enabled: microphoneSender.track?.enabled,
        readyState: microphoneSender.track?.readyState
      });
      
      // Check if sender track matches stream track
      if (microphoneSender.track === track) {
        console.log("✅ Sender track matches stream track");
      } else {
        console.log("❌ Sender track does NOT match stream track");
      }
    } else {
      console.log("❌ No WebRTC sender available");
    }
    
    // 5. Check peer connection
    if (peerConnection) {
      console.log("✅ Peer connection exists");
      console.log("Connection state:", peerConnection.connectionState);
      console.log("ICE connection state:", peerConnection.iceConnectionState);
      
      const transceivers = peerConnection.getTransceivers();
      const audioTransceivers = transceivers.filter(t => 
        t.sender.track?.kind === 'audio' || t.receiver.track?.kind === 'audio'
      );
      
      console.log("Audio transceivers:", audioTransceivers.map(t => ({
        direction: t.direction,
        senderTrack: t.sender.track?.id,
        receiverTrack: t.receiver.track?.id
      })));
    } else {
      console.log("❌ No peer connection available");
    }
    
  }, [microphoneSender, peerConnection]);

  // Make debug functions available globally for console access
  useEffect(() => {
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
    }).debugMicrophone = debugMicrophoneState;
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
    }).checkAudioStats = checkAudioTransmissionStats;
    (window as Window & { 
      debugMicrophone?: () => unknown;
      checkAudioStats?: () => unknown;
      testMicrophoneAudio?: () => unknown;
    }).testMicrophoneAudio = testMicrophoneAudio;
    return () => {
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
      }).debugMicrophone;
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
      }).checkAudioStats;
      delete (window as Window & { 
        debugMicrophone?: () => unknown;
        checkAudioStats?: () => unknown;
        testMicrophoneAudio?: () => unknown;
      }).testMicrophoneAudio;
    };
  }, [debugMicrophoneState, checkAudioTransmissionStats, testMicrophoneAudio]);

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
        console.log("Cleanup: stopping microphone stream on unmount");
        stream.getAudioTracks().forEach(track => {
          track.stop();
          console.log(`Cleanup: stopped audio track ${track.id}`);
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
    syncMicrophoneState,
    debugMicrophoneState,
  };
}