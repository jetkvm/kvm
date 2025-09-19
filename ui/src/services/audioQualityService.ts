import { JsonRpcResponse } from '@/hooks/useJsonRpc';

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

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (resp: JsonRpcResponse) => void) => void;

class AudioQualityService {
  private audioPresets: QualityPresets | null = null;
  private microphonePresets: QualityPresets | null = null;
  private qualityLabels: Record<number, string> = {
    0: 'Low',
    1: 'Medium',
    2: 'High',
    3: 'Ultra'
  };
  private rpcSend: RpcSendFunction | null = null;

  /**
   * Set RPC send function for cloud compatibility
   */
  setRpcSend(rpcSend: RpcSendFunction): void {
    this.rpcSend = rpcSend;
  }

  /**
   * Fetch audio quality presets using RPC (cloud-compatible)
   */
  async fetchAudioQualityPresets(): Promise<AudioQualityResponse | null> {
    if (!this.rpcSend) {
      console.error('RPC not available for audio quality presets');
      return null;
    }

    try {
      return await new Promise<AudioQualityResponse | null>((resolve) => {
        this.rpcSend!("audioQualityPresets", {}, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            console.error('RPC audio quality presets failed:', resp.error);
            resolve(null);
          } else if ("result" in resp) {
            const data = resp.result as AudioQualityResponse;
            this.audioPresets = data.presets;
            this.updateQualityLabels(data.presets);
            resolve(data);
          } else {
            resolve(null);
          }
        });
      });
    } catch (error) {
      console.error('Failed to fetch audio quality presets:', error);
      return null;
    }
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
   * Set audio quality using RPC (cloud-compatible)
   */
  async setAudioQuality(quality: number): Promise<boolean> {
    if (!this.rpcSend) {
      console.error('RPC not available for audio quality change');
      return false;
    }

    try {
      return await new Promise<boolean>((resolve) => {
        this.rpcSend!("audioQuality", { quality }, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            console.error('RPC audio quality change failed:', resp.error);
            resolve(false);
          } else {
            resolve(true);
          }
        });
      });
    } catch (error) {
      console.error('Failed to set audio quality:', error);
      return false;
    }
  }

  /**
   * Load both audio and microphone configurations
   */
  async loadAllConfigurations(): Promise<{
    audio: AudioQualityResponse | null;
  }> {
    const [audio ] = await Promise.all([
      this.fetchAudioQualityPresets(),
    ]);

    return { audio };
  }
}

// Export a singleton instance
export const audioQualityService = new AudioQualityService();
export default audioQualityService;