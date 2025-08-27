import api from '@/api';

interface AudioConfig {
  Quality: number;
  Bitrate: number;
  SampleRate: number;
  Channels: number;
  FrameSize: string;
}

type QualityPresets = Record<number, AudioConfig>;

interface AudioQualityResponse {
  current: AudioConfig;
  presets: QualityPresets;
}

class AudioQualityService {
  private audioPresets: QualityPresets | null = null;
  private microphonePresets: QualityPresets | null = null;
  private qualityLabels: Record<number, string> = {
    0: 'Low',
    1: 'Medium',
    2: 'High',
    3: 'Ultra'
  };

  /**
   * Fetch audio quality presets from the backend
   */
  async fetchAudioQualityPresets(): Promise<AudioQualityResponse | null> {
    try {
      const response = await api.GET('/audio/quality');
      if (response.ok) {
        const data = await response.json();
        this.audioPresets = data.presets;
        this.updateQualityLabels(data.presets);
        return data;
      }
    } catch (error) {
      console.error('Failed to fetch audio quality presets:', error);
    }
    return null;
  }

  /**
   * Fetch microphone quality presets from the backend
   */
  async fetchMicrophoneQualityPresets(): Promise<AudioQualityResponse | null> {
    try {
      const response = await api.GET('/microphone/quality');
      if (response.ok) {
        const data = await response.json();
        this.microphonePresets = data.presets;
        return data;
      }
    } catch (error) {
      console.error('Failed to fetch microphone quality presets:', error);
    }
    return null;
  }

  /**
   * Update quality labels with actual bitrates from presets
   */
  private updateQualityLabels(presets: QualityPresets): void {
    const newQualityLabels: Record<number, string> = {};
    Object.entries(presets).forEach(([qualityNum, preset]) => {
      const quality = parseInt(qualityNum);
      const qualityNames = ['Low', 'Medium', 'High', 'Ultra'];
      const name = qualityNames[quality] || `Quality ${quality}`;
      newQualityLabels[quality] = `${name} (${preset.Bitrate}kbps)`;
    });
    this.qualityLabels = newQualityLabels;
  }

  /**
   * Get quality labels with bitrates
   */
  getQualityLabels(): Record<number, string> {
    return this.qualityLabels;
  }

  /**
   * Get cached audio presets
   */
  getAudioPresets(): QualityPresets | null {
    return this.audioPresets;
  }

  /**
   * Get cached microphone presets
   */
  getMicrophonePresets(): QualityPresets | null {
    return this.microphonePresets;
  }

  /**
   * Set audio quality
   */
  async setAudioQuality(quality: number): Promise<boolean> {
    try {
      const response = await api.POST('/audio/quality', { quality });
      return response.ok;
    } catch (error) {
      console.error('Failed to set audio quality:', error);
      return false;
    }
  }

  /**
   * Set microphone quality
   */
  async setMicrophoneQuality(quality: number): Promise<boolean> {
    try {
      const response = await api.POST('/microphone/quality', { quality });
      return response.ok;
    } catch (error) {
      console.error('Failed to set microphone quality:', error);
      return false;
    }
  }

  /**
   * Load both audio and microphone configurations
   */
  async loadAllConfigurations(): Promise<{
    audio: AudioQualityResponse | null;
    microphone: AudioQualityResponse | null;
  }> {
    const [audio, microphone] = await Promise.all([
      this.fetchAudioQualityPresets(),
      this.fetchMicrophoneQualityPresets()
    ]);

    return { audio, microphone };
  }
}

// Export a singleton instance
export const audioQualityService = new AudioQualityService();
export default audioQualityService;