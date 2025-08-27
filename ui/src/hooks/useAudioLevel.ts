import { useEffect, useRef, useState } from 'react';

import { AUDIO_CONFIG } from '@/config/constants';

interface AudioLevelHookResult {
  audioLevel: number; // 0-100 percentage
  isAnalyzing: boolean;
}

interface AudioLevelOptions {
  enabled?: boolean; // Allow external control of analysis
  updateInterval?: number; // Throttle updates (default from AUDIO_CONFIG)
}

export const useAudioLevel = (
  stream: MediaStream | null, 
  options: AudioLevelOptions = {}
): AudioLevelHookResult => {
  const { enabled = true, updateInterval = AUDIO_CONFIG.LEVEL_UPDATE_INTERVAL } = options;
  
  const [audioLevel, setAudioLevel] = useState(0);
  const [isAnalyzing, setIsAnalyzing] = useState(false);
  const audioContextRef = useRef<AudioContext | null>(null);
  const analyserRef = useRef<AnalyserNode | null>(null);
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const intervalRef = useRef<number | null>(null);
  const lastUpdateTimeRef = useRef<number>(0);

  useEffect(() => {
    if (!stream || !enabled) {
      // Clean up when stream is null or disabled
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      if (sourceRef.current) {
        sourceRef.current.disconnect();
        sourceRef.current = null;
      }
      if (audioContextRef.current) {
        audioContextRef.current.close();
        audioContextRef.current = null;
      }
      analyserRef.current = null;
      setIsAnalyzing(false);
      setAudioLevel(0);
      return;
    }

    const audioTracks = stream.getAudioTracks();
    if (audioTracks.length === 0) {
      setIsAnalyzing(false);
      setAudioLevel(0);
      return;
    }

    try {
      // Create audio context and analyser
      const audioContext = new (window.AudioContext || (window as Window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext)();
      const analyser = audioContext.createAnalyser();
      const source = audioContext.createMediaStreamSource(stream);

      // Configure analyser - use smaller FFT for better performance
      analyser.fftSize = AUDIO_CONFIG.FFT_SIZE;
      analyser.smoothingTimeConstant = AUDIO_CONFIG.SMOOTHING_TIME_CONSTANT;
      
      // Connect nodes
      source.connect(analyser);

      // Store references
      audioContextRef.current = audioContext;
      analyserRef.current = analyser;
      sourceRef.current = source;

      const dataArray = new Uint8Array(analyser.frequencyBinCount);

      const updateLevel = () => {
        if (!analyserRef.current) return;

        const now = performance.now();
        
        // Throttle updates to reduce CPU usage
        if (now - lastUpdateTimeRef.current < updateInterval) {
          return;
        }
        lastUpdateTimeRef.current = now;

        analyserRef.current.getByteFrequencyData(dataArray);
        
        // Optimized RMS calculation - process only relevant frequency bands
        let sum = 0;
        const relevantBins = Math.min(dataArray.length, AUDIO_CONFIG.RELEVANT_FREQUENCY_BINS);
        for (let i = 0; i < relevantBins; i++) {
          const value = dataArray[i];
          sum += value * value;
        }
        const rms = Math.sqrt(sum / relevantBins);
        
        // Convert to percentage (0-100) with better scaling
        const level = Math.min(AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE, Math.max(0, (rms / AUDIO_CONFIG.RMS_SCALING_FACTOR) * AUDIO_CONFIG.MAX_LEVEL_PERCENTAGE));
        setAudioLevel(Math.round(level));
      };

      setIsAnalyzing(true);
      
      // Use setInterval instead of requestAnimationFrame for more predictable timing
      intervalRef.current = window.setInterval(updateLevel, updateInterval);

    } catch {
      // Audio level analyzer creation failed - silently handle
      setIsAnalyzing(false);
      setAudioLevel(0);
    }

    // Cleanup function
    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      if (sourceRef.current) {
        sourceRef.current.disconnect();
        sourceRef.current = null;
      }
      if (audioContextRef.current) {
        audioContextRef.current.close();
        audioContextRef.current = null;
      }
      analyserRef.current = null;
      setIsAnalyzing(false);
      setAudioLevel(0);
    };
  }, [stream, enabled, updateInterval]);

  return { audioLevel, isAnalyzing };
};