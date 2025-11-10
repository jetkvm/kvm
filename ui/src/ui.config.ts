export const CLOUD_API = import.meta.env.VITE_CLOUD_API;

export const DOWNGRADE_VERSION = import.meta.env.VITE_DOWNGRADE_VERSION || "0.4.8";

// In device mode, an empty string uses the current hostname (the JetKVM device's IP) as the API endpoint
export const DEVICE_API = "";
