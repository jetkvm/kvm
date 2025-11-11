export const CLOUD_API = import.meta.env.VITE_CLOUD_API;

export const DOWNGRADE_VERSION = import.meta.env.VITE_DOWNGRADE_VERSION || "0.4.8";

// In device mode, an empty string uses the current hostname (the JetKVM device's IP) as the API endpoint
export const DEVICE_API = "";

// Opus codec parameters for stereo audio with error correction
export const OPUS_STEREO_PARAMS = 'stereo=1;sprop-stereo=1;maxaveragebitrate=128000;usedtx=1;useinbandfec=1';
