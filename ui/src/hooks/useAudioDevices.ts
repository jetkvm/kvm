import { useState, useEffect, useCallback } from 'react';

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
      
      console.log('Audio devices enumerated:', {
        inputs: inputDevices.length,
        outputs: outputDevices.length
      });
      
    } catch (err) {
      console.error('Failed to enumerate audio devices:', err);
      setError(err instanceof Error ? err.message : 'Failed to access audio devices');
    } finally {
      setIsLoading(false);
    }
  }, []);

  // Listen for device changes
  useEffect(() => {
    const handleDeviceChange = () => {
      console.log('Audio devices changed, refreshing...');
      refreshDevices();
    };

    navigator.mediaDevices.addEventListener('devicechange', handleDeviceChange);
    
    // Initial load
    refreshDevices();

    return () => {
      navigator.mediaDevices.removeEventListener('devicechange', handleDeviceChange);
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