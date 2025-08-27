// Centralized configuration constants

// Network and API Configuration
export const NETWORK_CONFIG = {
  WEBSOCKET_RECONNECT_INTERVAL: 3000,
  LONG_PRESS_DURATION: 3000,
  ERROR_MESSAGE_TIMEOUT: 3000,
  AUDIO_TEST_DURATION: 5000,
  BACKEND_RETRY_DELAY: 500,
  RESET_DELAY: 200,
  STATE_CHECK_DELAY: 100,
  VERIFICATION_DELAY: 1000,
} as const;

// Default URLs and Endpoints
export const DEFAULT_URLS = {
  JETKVM_PROD_API: "https://api.jetkvm.com",
  JETKVM_PROD_APP: "https://app.jetkvm.com",
  JETKVM_DOCS_TROUBLESHOOTING: "https://jetkvm.com/docs/getting-started/troubleshooting",
  JETKVM_DOCS_REMOTE_ACCESS: "https://jetkvm.com/docs/networking/remote-access",
  JETKVM_DOCS_LOCAL_ACCESS_RESET: "https://jetkvm.com/docs/networking/local-access#reset-password",
  JETKVM_GITHUB: "https://github.com/jetkvm",
  CRONTAB_GURU: "https://crontab.guru/examples.html",
} as const;

// Sample ISO URLs for mounting
export const SAMPLE_ISOS = {
  UBUNTU_24_04: {
    name: "Ubuntu 24.04.2 Desktop",
    url: "https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-desktop-amd64.iso",
  },
  DEBIAN_13: {
    name: "Debian 13.0.0 (Testing)",
    url: "https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/debian-13.0.0-amd64-netinst.iso",
  },
  DEBIAN_12: {
    name: "Debian 12.11.0 (Stable)",
    url: "https://cdimage.debian.org/mirror/cdimage/archive/12.11.0/amd64/iso-cd/debian-12.11.0-amd64-netinst.iso",
  },
  FEDORA_41: {
    name: "Fedora 41 Workstation",
    url: "https://download.fedoraproject.org/pub/fedora/linux/releases/41/Workstation/x86_64/iso/Fedora-Workstation-Live-x86_64-41-1.4.iso",
  },
  OPENSUSE_LEAP: {
    name: "openSUSE Leap 15.6",
    url: "https://download.opensuse.org/distribution/leap/15.6/iso/openSUSE-Leap-15.6-NET-x86_64-Media.iso",
  },
  OPENSUSE_TUMBLEWEED: {
    name: "openSUSE Tumbleweed",
    url: "https://download.opensuse.org/tumbleweed/iso/openSUSE-Tumbleweed-NET-x86_64-Current.iso",
  },
  ARCH_LINUX: {
    name: "Arch Linux",
    url: "https://archlinux.doridian.net/iso/2025.02.01/archlinux-2025.02.01-x86_64.iso",
  },
  NETBOOT_XYZ: {
    name: "netboot.xyz",
    url: "https://boot.netboot.xyz/ipxe/netboot.xyz.iso",
  },
} as const;

// Security and Access Configuration
export const SECURITY_CONFIG = {
  LOCALHOST_ONLY_IP: "127.0.0.1",
  LOCALHOST_HOSTNAME: "localhost",
  HTTPS_PROTOCOL: "https:",
} as const;

// Default Hardware Configuration
export const HARDWARE_CONFIG = {
  DEFAULT_OFF_AFTER: 50000,
  SAMPLE_EDID: "00FFFFFFFFFFFF00047265058A3F6101101E0104A53420783FC125A8554EA0260D5054BFEF80714F8140818081C081008B009500B300283C80A070B023403020360006442100001A000000FD00304C575716010A202020202020000000FC0042323436574C0A202020202020000000FF0054384E4545303033383532320A01F802031CF14F90020304050607011112131415161F2309070783010000011D8018711C1620582C250006442100009E011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E9600064421000018C344806E70B028401720A80406442100001E00000000000000000000000000000000000000000000000000000096",
} as const;

// Audio Configuration
export const AUDIO_CONFIG = {
  // Audio Level Analysis
  LEVEL_UPDATE_INTERVAL: 100, // ms - throttle audio level updates for performance
  FFT_SIZE: 128, // reduced from 256 for better performance
  SMOOTHING_TIME_CONSTANT: 0.8,
  RELEVANT_FREQUENCY_BINS: 32, // focus on lower frequencies for voice
  RMS_SCALING_FACTOR: 180, // for converting RMS to percentage
  MAX_LEVEL_PERCENTAGE: 100,
  
  // Microphone Configuration
  SAMPLE_RATE: 48000, // Hz - high quality audio sampling
  CHANNEL_COUNT: 1, // mono for microphone input
  OPERATION_DEBOUNCE_MS: 1000, // debounce microphone operations
  SYNC_DEBOUNCE_MS: 1000, // debounce state synchronization
  AUDIO_TEST_TIMEOUT: 100, // ms - timeout for audio testing
  
  // NOTE: Audio quality presets (bitrates, sample rates, channels, frame sizes)
  // are now fetched dynamically from the backend API via audioQualityService
  // to eliminate duplication with backend config_constants.go
  
  // Default Quality Labels - will be updated dynamically by audioQualityService
  DEFAULT_QUALITY_LABELS: {
    0: "Low",
    1: "Medium",
    2: "High",
    3: "Ultra",
  } as const,
  
  // Audio Analysis
  ANALYSIS_FFT_SIZE: 256, // for detailed audio analysis
  ANALYSIS_UPDATE_INTERVAL: 100, // ms - 10fps for audio level updates
  LEVEL_SCALING_FACTOR: 255, // for RMS to percentage conversion
  
  // Audio Metrics Thresholds
  DROP_RATE_WARNING_THRESHOLD: 1, // percentage - yellow warning
  DROP_RATE_CRITICAL_THRESHOLD: 5, // percentage - red critical
  PERCENTAGE_MULTIPLIER: 100, // for converting ratios to percentages
  PERCENTAGE_DECIMAL_PLACES: 2, // decimal places for percentage display
} as const;

// Placeholder URLs
export const PLACEHOLDERS = {
  ISO_URL: "https://example.com/image.iso",
  PROXY_URL: "http://proxy.example.com:8080/",
  API_URL: "https://api.example.com",
  APP_URL: "https://app.example.com",
} as const;