import { useEffect, useRef, useState } from 'react';

interface AudioLevelHookResult {
  audioLevel: number; // 0-100 percentage
  isAnalyzing: boolean;
}

interface AudioLevelOptions {
  enabled?: boolean; // Allow external control of analysis
  updateInterval?: number; // Throttle updates (default: 100ms for 10fps instead of 60fps)
}

export const useAudioLevel = (
  stream: MediaStream | null, 
  options: AudioLevelOptions = {}
): AudioLevelHookResult => {
  const { enabled = true, updateInterval = 100 } = options;
  
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
      analyser.fftSize = 128; // Reduced from 256 for better performance
      analyser.smoothingTimeConstant = 0.8;
      
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
        const relevantBins = Math.min(dataArray.length, 32); // Focus on lower frequencies for voice
        for (let i = 0; i < relevantBins; i++) {
          const value = dataArray[i];
          sum += value * value;
        }
        const rms = Math.sqrt(sum / relevantBins);
        
        // Convert to percentage (0-100) with better scaling
        const level = Math.min(100, Math.max(0, (rms / 180) * 100)); // Adjusted scaling for better sensitivity
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