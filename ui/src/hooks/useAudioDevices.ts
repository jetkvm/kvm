import { useState, useEffect, useCallback } from 'react';

import { devError } from '../utils/debug';

export interface AudioDevice {
  deviceId: string;
  label: string;
  kind: 'audioinput' | 'audiooutput';
}

export interface UseAudioDevicesReturn {
  audioInputDevices: AudioDevice[];
  audioOutputDevices: AudioDevice[];
  selectedInputDevice: string;
  selectedOutputDevice: string;
  isLoading: boolean;
  error: string | null;
  refreshDevices: () => Promise<void>;
  setSelectedInputDevice: (deviceId: string) => void;
  setSelectedOutputDevice: (deviceId: string) => void;
}

export function useAudioDevices(): UseAudioDevicesReturn {
  const [audioInputDevices, setAudioInputDevices] = useState<AudioDevice[]>([]);
  const [audioOutputDevices, setAudioOutputDevices] = useState<AudioDevice[]>([]);
  const [selectedInputDevice, setSelectedInputDevice] = useState<string>('default');
  const [selectedOutputDevice, setSelectedOutputDevice] = useState<string>('default');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refreshDevices = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    
    try {
      // Check if we're on HTTP (microphone requires HTTPS, but speakers can work)
      const isHttp = window.location.protocol === 'http:';
      const hasMediaDevices = !!navigator.mediaDevices;
      const hasGetUserMedia = !!navigator.mediaDevices?.getUserMedia;
      const hasEnumerateDevices = !!navigator.mediaDevices?.enumerateDevices;
      
      if (isHttp || !hasMediaDevices || !hasGetUserMedia) {
        // Set placeholder devices when HTTPS is required for microphone
        setAudioInputDevices([
          { deviceId: 'https-required', label: 'HTTPS Required for Microphone Access', kind: 'audioinput' }
        ]);
        
        // Try to enumerate speakers if possible, otherwise provide defaults
        if (hasMediaDevices && hasEnumerateDevices) {
          try {
            const devices = await navigator.mediaDevices.enumerateDevices();
            const outputDevices: AudioDevice[] = [
              { deviceId: 'default', label: 'Default Speaker', kind: 'audiooutput' }
            ];
            
            devices.forEach(device => {
              if (device.kind === 'audiooutput' && device.deviceId !== 'default') {
                outputDevices.push({
                  deviceId: device.deviceId,
                  label: device.label || `Speaker ${device.deviceId.slice(0, 8)}`,
                  kind: 'audiooutput'
                });
              }
            });
            
            setAudioOutputDevices(outputDevices);
          } catch {
            // Fallback to default speakers if enumeration fails
            setAudioOutputDevices([
              { deviceId: 'default', label: 'Default Speaker', kind: 'audiooutput' },
              { deviceId: 'system-default', label: 'System Default Audio Output', kind: 'audiooutput' }
            ]);
          }
        } else {
          // No enumeration available, use defaults
          setAudioOutputDevices([
            { deviceId: 'default', label: 'Default Speaker', kind: 'audiooutput' },
            { deviceId: 'system-default', label: 'System Default Audio Output', kind: 'audiooutput' }
          ]);
        }
        
        setSelectedInputDevice('https-required');
        setSelectedOutputDevice('default');
        return; // Exit gracefully without throwing error on HTTP
      }
      
      // Request permissions first to get device labels
      await navigator.mediaDevices.getUserMedia({ audio: true });
      
      const devices = await navigator.mediaDevices.enumerateDevices();
      
      const inputDevices: AudioDevice[] = [
        { deviceId: 'default', label: 'Default Microphone', kind: 'audioinput' }
      ];
      
      const outputDevices: AudioDevice[] = [
        { deviceId: 'default', label: 'Default Speaker', kind: 'audiooutput' }
      ];
      
      devices.forEach(device => {
        if (device.kind === 'audioinput' && device.deviceId !== 'default') {
          inputDevices.push({
            deviceId: device.deviceId,
            label: device.label || `Microphone ${device.deviceId.slice(0, 8)}`,
            kind: 'audioinput'
          });
        } else if (device.kind === 'audiooutput' && device.deviceId !== 'default') {
          outputDevices.push({
            deviceId: device.deviceId,
            label: device.label || `Speaker ${device.deviceId.slice(0, 8)}`,
            kind: 'audiooutput'
          });
        }
      });
      
      setAudioInputDevices(inputDevices);
      setAudioOutputDevices(outputDevices);
      
      // Audio devices enumerated
      
    } catch (err) {
      // Only log errors on HTTPS where we expect full device access
      const isHttp = window.location.protocol === 'http:';
      if (!isHttp) {
        devError('Failed to enumerate audio devices:', err);
      }
      
      let errorMessage = 'Failed to access audio devices';
      
      if (err instanceof Error) {
        if (err.message.includes('HTTPS')) {
          errorMessage = err.message;
        } else if (err.name === 'NotAllowedError' || err.name === 'PermissionDeniedError') {
          errorMessage = 'Microphone permission denied. Please allow microphone access.';
        } else if (err.name === 'NotFoundError' || err.name === 'DevicesNotFoundError') {
          errorMessage = 'No microphone devices found.';
        } else if (err.name === 'NotSupportedError') {
          errorMessage = 'Audio devices are not supported on this connection. Please use HTTPS.';
        } else {
          errorMessage = err.message || errorMessage;
        }
      }
      
      // Only set error state on HTTPS where we expect device access to work
      if (!isHttp) {
        setError(errorMessage);
      }
    } finally {
      setIsLoading(false);
    }
  }, []);

  // Listen for device changes
  useEffect(() => {
    const handleDeviceChange = () => {
      // Audio devices changed, refreshing
      refreshDevices();
    };

    // Check if navigator.mediaDevices exists and supports addEventListener
    if (navigator.mediaDevices && typeof navigator.mediaDevices.addEventListener === 'function') {
      navigator.mediaDevices.addEventListener('devicechange', handleDeviceChange);
    }
    
    // Initial load
    refreshDevices();

    return () => {
      // Check if navigator.mediaDevices exists and supports removeEventListener
      if (navigator.mediaDevices && typeof navigator.mediaDevices.removeEventListener === 'function') {
        navigator.mediaDevices.removeEventListener('devicechange', handleDeviceChange);
      }
    };
  }, [refreshDevices]);

  return {
    audioInputDevices,
    audioOutputDevices,
    selectedInputDevice,
    selectedOutputDevice,
    isLoading,
    error,
    refreshDevices,
    setSelectedInputDevice,
    setSelectedOutputDevice,
  };
}