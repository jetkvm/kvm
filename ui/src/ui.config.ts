interface JetKVMConfig {
  CLOUD_API?: string;
  CLOUD_APP?: string;
  DEVICE_VERSION?: string;
}

declare global {
  interface Window { JETKVM_CONFIG?: JetKVMConfig; }
}

const getAppURL = (api_url?: string) => {
  if (!api_url) {
    return;
  }
  const url = new URL(api_url);
  url.host = url.host.replace(/api\./, "app.");
  // remove the ending slash
  return url.toString().replace(/\/$/, "");
}

export const CLOUD_API = window.JETKVM_CONFIG?.CLOUD_API || import.meta.env.VITE_CLOUD_API;
export const CLOUD_APP = window.JETKVM_CONFIG?.CLOUD_APP || getAppURL(CLOUD_API) || import.meta.env.VITE_CLOUD_APP;
export const SIGNAL_API = import.meta.env.VITE_SIGNAL_API;
