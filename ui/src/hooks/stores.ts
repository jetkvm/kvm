import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { MAX_STEPS_PER_MACRO, MAX_TOTAL_MACROS, MAX_KEYS_PER_STEP } from "@/constants/macros";

// Define the JsonRpc types for better type checking
interface JsonRpcResponse {
  jsonrpc: string;
  result?: unknown;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
  id: number | string | null;
}

// Utility function to append stats to a Map
const appendStatToMap = <T extends { timestamp: number }>(
  stat: T,
  prevMap: Map<number, T>,
  maxEntries = 130,
): Map<number, T> => {
  if (prevMap.size > maxEntries) {
    const firstKey = prevMap.keys().next().value;
    if (firstKey !== undefined) {
      prevMap.delete(firstKey);
    }
  }

  const date = Math.floor(stat.timestamp / 1000);
  const newStat = { ...prevMap.get(date), ...stat };
  return new Map(prevMap).set(date, newStat);
};

// Constants and types
export type AvailableSidebarViews = "connection-stats";
export type AvailableTerminalTypes = "kvm" | "serial" | "none";

export interface User {
  sub: string;
  email?: string;
  picture?: string;
}

interface UserState {
  user: User | null;
  setUser: (user: User | null) => void;
}

interface UIState {
  sidebarView: AvailableSidebarViews | null;
  setSidebarView: (view: AvailableSidebarViews | null) => void;

  disableVideoFocusTrap: boolean;
  setDisableVideoFocusTrap: (enabled: boolean) => void;

  isWakeOnLanModalVisible: boolean;
  setWakeOnLanModalVisibility: (enabled: boolean) => void;

  toggleSidebarView: (view: AvailableSidebarViews) => void;

  isAttachedVirtualKeyboardVisible: boolean;
  setAttachedVirtualKeyboardVisibility: (enabled: boolean) => void;

  terminalType: AvailableTerminalTypes;
  setTerminalType: (enabled: UIState["terminalType"]) => void;
}

export const useUiStore = create<UIState>(set => ({
  terminalType: "none",
  setTerminalType: type => set({ terminalType: type }),

  sidebarView: null,
  setSidebarView: view => set({ sidebarView: view }),

  disableVideoFocusTrap: false,
  setDisableVideoFocusTrap: enabled => set({ disableVideoFocusTrap: enabled }),

  isWakeOnLanModalVisible: false,
  setWakeOnLanModalVisibility: enabled => set({ isWakeOnLanModalVisible: enabled }),

  toggleSidebarView: view =>
    set(state => {
      if (state.sidebarView === view) {
        return { sidebarView: null };
      } else {
        return { sidebarView: view };
      }
    }),

  isAttachedVirtualKeyboardVisible: true,
  setAttachedVirtualKeyboardVisibility: enabled =>
    set({ isAttachedVirtualKeyboardVisible: enabled }),
}));

interface RTCState {
  peerConnection: RTCPeerConnection | null;
  setPeerConnection: (pc: RTCState["peerConnection"]) => void;

  setRpcDataChannel: (channel: RTCDataChannel) => void;
  rpcDataChannel: RTCDataChannel | null;

  diskChannel: RTCDataChannel | null;
  setDiskChannel: (channel: RTCDataChannel) => void;

  peerConnectionState: RTCPeerConnectionState | null;
  setPeerConnectionState: (state: RTCPeerConnectionState) => void;

  transceiver: RTCRtpTransceiver | null;
  setTransceiver: (transceiver: RTCRtpTransceiver) => void;

  mediaStream: MediaStream | null;
  setMediaStream: (stream: MediaStream) => void;

  videoStreamStats: RTCInboundRtpStreamStats | null;
  appendVideoStreamStats: (state: RTCInboundRtpStreamStats) => void;
  videoStreamStatsHistory: Map<number, RTCInboundRtpStreamStats>;

  isTurnServerInUse: boolean;
  setTurnServerInUse: (inUse: boolean) => void;

  inboundRtpStats: Map<number, RTCInboundRtpStreamStats>;
  appendInboundRtpStats: (state: RTCInboundRtpStreamStats) => void;
  clearInboundRtpStats: () => void;

  candidatePairStats: Map<number, RTCIceCandidatePairStats>;
  appendCandidatePairStats: (pair: RTCIceCandidatePairStats) => void;
  clearCandidatePairStats: () => void;

  // Remote ICE candidates stat type doesn't exist as of today
  localCandidateStats: Map<number, RTCIceCandidateStats>;
  appendLocalCandidateStats: (stats: RTCIceCandidateStats) => void;

  remoteCandidateStats: Map<number, RTCIceCandidateStats>;
  appendRemoteCandidateStats: (stats: RTCIceCandidateStats) => void;

  // Disk data channel stats type doesn't exist as of today
  diskDataChannelStats: Map<number, RTCDataChannelStats>;
  appendDiskDataChannelStats: (stat: RTCDataChannelStats) => void;

  terminalChannel: RTCDataChannel | null;
  setTerminalChannel: (channel: RTCDataChannel) => void;
}

export const useRTCStore = create<RTCState>(set => ({
  peerConnection: null,
  setPeerConnection: pc => set({ peerConnection: pc }),

  rpcDataChannel: null,
  setRpcDataChannel: channel => set({ rpcDataChannel: channel }),

  transceiver: null,
  setTransceiver: transceiver => set({ transceiver }),

  peerConnectionState: null,
  setPeerConnectionState: state => set({ peerConnectionState: state }),

  diskChannel: null,
  setDiskChannel: channel => set({ diskChannel: channel }),

  mediaStream: null,
  setMediaStream: stream => set({ mediaStream: stream }),

  videoStreamStats: null,
  appendVideoStreamStats: stats => set({ videoStreamStats: stats }),
  videoStreamStatsHistory: new Map(),

  isTurnServerInUse: false,
  setTurnServerInUse: inUse => set({ isTurnServerInUse: inUse }),

  inboundRtpStats: new Map(),
  appendInboundRtpStats: newStat => {
    set(prevState => ({
      inboundRtpStats: appendStatToMap(newStat, prevState.inboundRtpStats),
    }));
  },
  clearInboundRtpStats: () => set({ inboundRtpStats: new Map() }),

  candidatePairStats: new Map(),
  appendCandidatePairStats: newStat => {
    set(prevState => ({
      candidatePairStats: appendStatToMap(newStat, prevState.candidatePairStats),
    }));
  },
  clearCandidatePairStats: () => set({ candidatePairStats: new Map() }),

  localCandidateStats: new Map(),
  appendLocalCandidateStats: newStat => {
    set(prevState => ({
      localCandidateStats: appendStatToMap(newStat, prevState.localCandidateStats),
    }));
  },

  remoteCandidateStats: new Map(),
  appendRemoteCandidateStats: newStat => {
    set(prevState => ({
      remoteCandidateStats: appendStatToMap(newStat, prevState.remoteCandidateStats),
    }));
  },

  diskDataChannelStats: new Map(),
  appendDiskDataChannelStats: newStat => {
    set(prevState => ({
      diskDataChannelStats: appendStatToMap(newStat, prevState.diskDataChannelStats),
    }));
  },

  // Add these new properties to the store implementation
  terminalChannel: null,
  setTerminalChannel: channel => set({ terminalChannel: channel }),
}));

interface MouseMove {
  x: number;
  y: number;
  buttons: number;
}
interface MouseState {
  mouseX: number;
  mouseY: number;
  mouseMove?: MouseMove;
  setMouseMove: (move?: MouseMove) => void;
  setMousePosition: (x: number, y: number) => void;
}

export const useMouseStore = create<MouseState>(set => ({
  mouseX: 0,
  mouseY: 0,
  setMouseMove: (move?: MouseMove) => set({ mouseMove: move }),
  setMousePosition: (x, y) => set({ mouseX: x, mouseY: y }),
}));

export interface VideoState {
  width: number;
  height: number;
  clientWidth: number;
  clientHeight: number;
  setClientSize: (width: number, height: number) => void;
  setSize: (width: number, height: number) => void;
  hdmiState: "ready" | "no_signal" | "no_lock" | "out_of_range" | "connecting";
  setHdmiState: (state: {
    ready: boolean;
    error?: Extract<VideoState["hdmiState"], "no_signal" | "no_lock" | "out_of_range">;
  }) => void;
}

export interface BacklightSettings {
  max_brightness: number;
  dim_after: number;
  off_after: number;
}

export const useVideoStore = create<VideoState>(set => ({
  width: 0,
  height: 0,

  clientWidth: 0,
  clientHeight: 0,

  // The video element's client size
  setClientSize: (clientWidth, clientHeight) => set({ clientWidth, clientHeight }),

  // Resolution
  setSize: (width, height) => set({ width, height }),

  hdmiState: "connecting",
  setHdmiState: state => {
    if (!state) return;
    const { ready, error } = state;

    if (ready) {
      return set({ hdmiState: "ready" });
    } else if (error) {
      return set({ hdmiState: error });
    } else {
      return set({ hdmiState: "connecting" });
    }
  },
}));

interface SettingsState {
  isCursorHidden: boolean;
  setCursorVisibility: (enabled: boolean) => void;

  mouseMode: string;
  setMouseMode: (mode: string) => void;

  debugMode: boolean;
  setDebugMode: (enabled: boolean) => void;

  // Add new developer mode state
  developerMode: boolean;
  setDeveloperMode: (enabled: boolean) => void;

  backlightSettings: BacklightSettings;
  setBacklightSettings: (settings: BacklightSettings) => void;
}

export const useSettingsStore = create(
  persist<SettingsState>(
    set => ({
      isCursorHidden: false,
      setCursorVisibility: enabled => set({ isCursorHidden: enabled }),

      mouseMode: "absolute",
      setMouseMode: mode => set({ mouseMode: mode }),

      debugMode: import.meta.env.DEV,
      setDebugMode: enabled => set({ debugMode: enabled }),

      // Add developer mode with default value
      developerMode: false,
      setDeveloperMode: enabled => set({ developerMode: enabled }),

      backlightSettings: {
        max_brightness: 100,
        dim_after: 10000,
        off_after: 50000,
      },
      setBacklightSettings: (settings: BacklightSettings) =>
        set({ backlightSettings: settings }),
    }),
    {
      name: "settings",
      storage: createJSONStorage(() => localStorage),
    },
  ),
);

export interface DeviceSettingsState {
  trackpadSensitivity: number;
  mouseSensitivity: number;
  clampMin: number;
  clampMax: number;
  blockDelay: number;
  trackpadThreshold: number;
  scrollSensitivity: "low" | "default" | "high";
  setScrollSensitivity: (sensitivity: DeviceSettingsState["scrollSensitivity"]) => void;
}

export const useDeviceSettingsStore = create<DeviceSettingsState>(set => ({
  trackpadSensitivity: 3.0,
  mouseSensitivity: 5.0,
  clampMin: -8,
  clampMax: 8,
  blockDelay: 25,
  trackpadThreshold: 10,

  scrollSensitivity: "default",
  setScrollSensitivity: sensitivity => {
    const wheelSettings: Record<
      DeviceSettingsState["scrollSensitivity"],
      {
        trackpadSensitivity: DeviceSettingsState["trackpadSensitivity"];
        mouseSensitivity: DeviceSettingsState["mouseSensitivity"];
        clampMin: DeviceSettingsState["clampMin"];
        clampMax: DeviceSettingsState["clampMax"];
        blockDelay: DeviceSettingsState["blockDelay"];
        trackpadThreshold: DeviceSettingsState["trackpadThreshold"];
      }
    > = {
      low: {
        trackpadSensitivity: 2.0,
        mouseSensitivity: 3.0,
        clampMin: -6,
        clampMax: 6,
        blockDelay: 30,
        trackpadThreshold: 10,
      },
      default: {
        trackpadSensitivity: 3.0,
        mouseSensitivity: 5.0,
        clampMin: -8,
        clampMax: 8,
        blockDelay: 25,
        trackpadThreshold: 10,
      },
      high: {
        trackpadSensitivity: 4.0,
        mouseSensitivity: 6.0,
        clampMin: -9,
        clampMax: 9,
        blockDelay: 20,
        trackpadThreshold: 10,
      },
    };

    const settings = wheelSettings[sensitivity];

    return set({
      trackpadSensitivity: settings.trackpadSensitivity,
      trackpadThreshold: settings.trackpadThreshold,
      mouseSensitivity: settings.mouseSensitivity,
      clampMin: settings.clampMin,
      clampMax: settings.clampMax,
      blockDelay: settings.blockDelay,
      scrollSensitivity: sensitivity,
    });
  },
}));

export interface RemoteVirtualMediaState {
  source: "WebRTC" | "HTTP" | "Storage" | null;
  mode: "CDROM" | "Disk" | null;
  filename: string | null;
  url: string | null;
  path: string | null;
  size: number | null;
}

export interface MountMediaState {
  localFile: File | null;
  setLocalFile: (file: MountMediaState["localFile"]) => void;

  remoteVirtualMediaState: RemoteVirtualMediaState | null;
  setRemoteVirtualMediaState: (state: MountMediaState["remoteVirtualMediaState"]) => void;

  modalView: "mode" | "browser" | "url" | "device" | "upload" | "error" | null;
  setModalView: (view: MountMediaState["modalView"]) => void;

  isMountMediaDialogOpen: boolean;
  setIsMountMediaDialogOpen: (isOpen: MountMediaState["isMountMediaDialogOpen"]) => void;

  uploadedFiles: { name: string; size: string; uploadedAt: string }[];
  addUploadedFile: (file: { name: string; size: string; uploadedAt: string }) => void;

  errorMessage: string | null;
  setErrorMessage: (message: string | null) => void;
}

export const useMountMediaStore = create<MountMediaState>(set => ({
  localFile: null,
  setLocalFile: file => set({ localFile: file }),

  remoteVirtualMediaState: null,
  setRemoteVirtualMediaState: state => set({ remoteVirtualMediaState: state }),

  modalView: "mode",
  setModalView: view => set({ modalView: view }),

  isMountMediaDialogOpen: false,
  setIsMountMediaDialogOpen: isOpen => set({ isMountMediaDialogOpen: isOpen }),

  uploadedFiles: [],
  addUploadedFile: file =>
    set(state => ({ uploadedFiles: [...state.uploadedFiles, file] })),

  errorMessage: null,
  setErrorMessage: message => set({ errorMessage: message }),
}));

export interface HidState {
  activeKeys: number[];
  activeModifiers: number[];

  updateActiveKeysAndModifiers: (keysAndModifiers: {
    keys: number[];
    modifiers: number[];
  }) => void;

  altGrArmed: boolean;
  setAltGrArmed: (armed: boolean) => void;

  altGrTimer: number | null; // _altGrCtrlTime
  setAltGrTimer: (timeout: number | null) => void;

  altGrCtrlTime: number; // _altGrCtrlTime
  setAltGrCtrlTime: (time: number) => void;

  isNumLockActive: boolean;
  setIsNumLockActive: (enabled: boolean) => void;

  isScrollLockActive: boolean;
  setIsScrollLockActive: (enabled: boolean) => void;

  isVirtualKeyboardEnabled: boolean;
  setVirtualKeyboardEnabled: (enabled: boolean) => void;

  isCapsLockActive: boolean;
  setIsCapsLockActive: (enabled: boolean) => void;

  isPasteModeEnabled: boolean;
  setPasteModeEnabled: (enabled: boolean) => void;

  usbState: "configured" | "attached" | "not attached" | "suspended" | "addressed";
  setUsbState: (state: HidState["usbState"]) => void;
}

export const useHidStore = create<HidState>(set => ({
  activeKeys: [],
  activeModifiers: [],
  updateActiveKeysAndModifiers: ({ keys, modifiers }) => {
    return set({ activeKeys: keys, activeModifiers: modifiers });
  },

  altGrArmed: false,
  setAltGrArmed: armed => set({ altGrArmed: armed }),

  altGrTimer: 0,
  setAltGrTimer: timeout => set({ altGrTimer: timeout }),

  altGrCtrlTime: 0,
  setAltGrCtrlTime: time => set({ altGrCtrlTime: time }),

  isNumLockActive: false,
  setIsNumLockActive: enabled => set({ isNumLockActive: enabled }),

  isScrollLockActive: false,
  setIsScrollLockActive: enabled => set({ isScrollLockActive: enabled }),

  isVirtualKeyboardEnabled: false,
  setVirtualKeyboardEnabled: enabled => set({ isVirtualKeyboardEnabled: enabled }),

  isCapsLockActive: false,
  setIsCapsLockActive: enabled => set({ isCapsLockActive: enabled }),

  isPasteModeEnabled: false,
  setPasteModeEnabled: enabled => set({ isPasteModeEnabled: enabled }),

  // Add these new properties for USB state
  usbState: "not attached",
  setUsbState: state => set({ usbState: state }),
}));

export const useUserStore = create<UserState>(set => ({
  user: null,
  setUser: user => set({ user }),
}));

export interface UpdateState {
  isUpdatePending: boolean;
  setIsUpdatePending: (isPending: boolean) => void;
  updateDialogHasBeenMinimized: boolean;
  otaState: {
    updating: boolean;
    error: string | null;

    metadataFetchedAt: string | null;

    // App update
    appUpdatePending: boolean;

    appDownloadProgress: number;
    appDownloadFinishedAt: string | null;

    appVerificationProgress: number;
    appVerifiedAt: string | null;

    appUpdateProgress: number;
    appUpdatedAt: string | null;

    // System update
    systemUpdatePending: boolean;

    systemDownloadProgress: number;
    systemDownloadFinishedAt: string | null;

    systemVerificationProgress: number;
    systemVerifiedAt: string | null;

    systemUpdateProgress: number;
    systemUpdatedAt: string | null;
  };
  setOtaState: (state: UpdateState["otaState"]) => void;
  setUpdateDialogHasBeenMinimized: (hasBeenMinimized: boolean) => void;
  modalView:
  | "loading"
  | "updating"
  | "upToDate"
  | "updateAvailable"
  | "updateCompleted"
  | "error";
  setModalView: (view: UpdateState["modalView"]) => void;
  setUpdateErrorMessage: (errorMessage: string) => void;
  updateErrorMessage: string | null;
}

export const useUpdateStore = create<UpdateState>(set => ({
  isUpdatePending: false,
  setIsUpdatePending: isPending => set({ isUpdatePending: isPending }),

  setOtaState: state => set({ otaState: state }),
  otaState: {
    updating: false,
    error: null,
    metadataFetchedAt: null,
    appUpdatePending: false,
    systemUpdatePending: false,
    appDownloadProgress: 0,
    appDownloadFinishedAt: null,
    appVerificationProgress: 0,
    appVerifiedAt: null,
    systemDownloadProgress: 0,
    systemDownloadFinishedAt: null,
    systemVerificationProgress: 0,
    systemVerifiedAt: null,
    appUpdateProgress: 0,
    appUpdatedAt: null,
    systemUpdateProgress: 0,
    systemUpdatedAt: null,
  },

  updateDialogHasBeenMinimized: false,
  setUpdateDialogHasBeenMinimized: hasBeenMinimized =>
    set({ updateDialogHasBeenMinimized: hasBeenMinimized }),
  modalView: "loading",
  setModalView: view => set({ modalView: view }),
  updateErrorMessage: null,
  setUpdateErrorMessage: errorMessage => set({ updateErrorMessage: errorMessage }),
}));

interface UsbConfigModalState {
  modalView: "updateUsbConfig" | "updateUsbConfigSuccess";
  errorMessage: string | null;
  setModalView: (view: UsbConfigModalState["modalView"]) => void;
  setErrorMessage: (message: string | null) => void;
}

export interface UsbConfigState {
  vendor_id: string;
  product_id: string;
  serial_number: string;
  manufacturer: string;
  product: string;
}

export const useUsbConfigModalStore = create<UsbConfigModalState>(set => ({
  modalView: "updateUsbConfig",
  errorMessage: null,
  setModalView: view => set({ modalView: view }),
  setErrorMessage: message => set({ errorMessage: message }),
}));

interface LocalAuthModalState {
  modalView:
  | "createPassword"
  | "deletePassword"
  | "updatePassword"
  | "creationSuccess"
  | "deleteSuccess"
  | "updateSuccess";
  setModalView: (view: LocalAuthModalState["modalView"]) => void;
}

export const useLocalAuthModalStore = create<LocalAuthModalState>(set => ({
  modalView: "createPassword",
  setModalView: view => set({ modalView: view }),
}));

export interface DeviceState {
  appVersion: string | null;
  systemVersion: string | null;

  setAppVersion: (version: string) => void;
  setSystemVersion: (version: string) => void;
}

export const useDeviceStore = create<DeviceState>(set => ({
  appVersion: null,
  systemVersion: null,

  setAppVersion: version => set({ appVersion: version }),
  setSystemVersion: version => set({ systemVersion: version }),
}));

export interface DhcpLease {
  ip?: string;
  netmask?: string;
  broadcast?: string;
  ttl?: string;
  mtu?: string;
  hostname?: string;
  domain?: string;
  bootp_next_server?: string;
  bootp_server_name?: string;
  bootp_file?: string;
  timezone?: string;
  routers?: string[];
  dns?: string[];
  ntp_servers?: string[];
  lpr_servers?: string[];
  _time_servers?: string[];
  _name_servers?: string[];
  _log_servers?: string[];
  _cookie_servers?: string[];
  _wins_servers?: string[];
  _swap_server?: string;
  boot_size?: string;
  root_path?: string;
  lease?: string;
  lease_expiry?: Date;
  dhcp_type?: string;
  server_id?: string;
  message?: string;
  tftp?: string;
  bootfile?: string;
}

export interface IPv6Address {
  address: string;
  prefix: string;
  valid_lifetime: string;
  preferred_lifetime: string;
  scope: string;
}

export interface NetworkState {
  interface_name?: string;
  mac_address?: string;
  ipv4?: string;
  ipv4_addresses?: string[];
  ipv6?: string;
  ipv6_addresses?: IPv6Address[];
  ipv6_link_local?: string;
  dhcp_lease?: DhcpLease;

  setNetworkState: (state: NetworkState) => void;
  setDhcpLease: (lease: NetworkState["dhcp_lease"]) => void;
  setDhcpLeaseExpiry: (expiry: Date) => void;
}


export type IPv6Mode = "disabled" | "slaac" | "dhcpv6" | "slaac_and_dhcpv6" | "static" | "link_local" | "unknown";
export type IPv4Mode = "disabled" | "static" | "dhcp" | "unknown";
export type LLDPMode = "disabled" | "basic" | "all" | "unknown";
export type mDNSMode = "disabled" | "auto" | "ipv4_only" | "ipv6_only" | "unknown";
export type TimeSyncMode = "ntp_only" | "ntp_and_http" | "http_only" | "custom" | "unknown";

export interface NetworkSettings {
  hostname: string;
  domain: string;
  ipv4_mode: IPv4Mode;
  ipv6_mode: IPv6Mode;
  lldp_mode: LLDPMode;
  lldp_tx_tlvs: string[];
  mdns_mode: mDNSMode;
  time_sync_mode: TimeSyncMode;
}

export const useNetworkStateStore = create<NetworkState>((set, get) => ({
  setNetworkState: (state: NetworkState) => set(state),
  setDhcpLease: (lease: NetworkState["dhcp_lease"]) => set({ dhcp_lease: lease }),
  setDhcpLeaseExpiry: (expiry: Date) => {
    const lease = get().dhcp_lease;
    if (!lease) {
      console.warn("No lease found");
      return;
    }

    lease.lease_expiry = expiry;
    set({ dhcp_lease: lease });
  }
}));

export interface KeySequenceStep {
  keys: string[];
  modifiers: string[];
  delay: number;
}

export interface KeySequence {
  id: string;
  name: string;
  steps: KeySequenceStep[];
  sortOrder?: number;
}

export interface MacrosState {
  macros: KeySequence[];
  loading: boolean;
  initialized: boolean;
  loadMacros: () => Promise<void>;
  saveMacros: (macros: KeySequence[]) => Promise<void>;
  sendFn: ((method: string, params: unknown, callback?: ((resp: JsonRpcResponse) => void) | undefined) => void) | null;
  setSendFn: (sendFn: ((method: string, params: unknown, callback?: ((resp: JsonRpcResponse) => void) | undefined) => void)) => void;
}

export const generateMacroId = () => {
  return Math.random().toString(36).substring(2, 9);
};

export const useMacrosStore = create<MacrosState>((set, get) => ({
  macros: [],
  loading: false,
  initialized: false,
  sendFn: null,

  setSendFn: (sendFn) => {
    set({ sendFn });
  },

  loadMacros: async () => {
    if (get().initialized) return;

    const { sendFn } = get();
    if (!sendFn) {
      console.warn("JSON-RPC send function not available.");
      return;
    }

    set({ loading: true });

    try {
      await new Promise<void>((resolve, reject) => {
        sendFn("getKeyboardMacros", {}, (response) => {
          if (response.error) {
            console.error("Error loading macros:", response.error);
            reject(new Error(response.error.message));
            return;
          }

          const macros = (response.result as KeySequence[]) || [];

          const sortedMacros = [...macros].sort((a, b) => {
            if (a.sortOrder !== undefined && b.sortOrder !== undefined) {
              return a.sortOrder - b.sortOrder;
            }
            if (a.sortOrder !== undefined) return -1;
            if (b.sortOrder !== undefined) return 1;
            return 0;
          });

          set({
            macros: sortedMacros,
            initialized: true
          });

          resolve();
        });
      });
    } catch (error) {
      console.error("Failed to load macros:", error);
    } finally {
      set({ loading: false });
    }
  },

  saveMacros: async (macros: KeySequence[]) => {
    const { sendFn } = get();
    if (!sendFn) {
      console.warn("JSON-RPC send function not available.");
      throw new Error("JSON-RPC send function not available");
    }

    if (macros.length > MAX_TOTAL_MACROS) {
      console.error(`Cannot save: exceeded maximum of ${MAX_TOTAL_MACROS} macros`);
      throw new Error(`Cannot save: exceeded maximum of ${MAX_TOTAL_MACROS} macros`);
    }

    for (const macro of macros) {
      if (macro.steps.length > MAX_STEPS_PER_MACRO) {
        console.error(`Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`);
        throw new Error(`Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`);
      }

      for (let i = 0; i < macro.steps.length; i++) {
        const step = macro.steps[i];
        if (step.keys && step.keys.length > MAX_KEYS_PER_STEP) {
          console.error(`Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`);
          throw new Error(`Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`);
        }
      }
    }

    set({ loading: true });

    try {
      const macrosWithSortOrder = macros.map((macro, index) => ({
        ...macro,
        sortOrder: macro.sortOrder !== undefined ? macro.sortOrder : index
      }));

      const response = await new Promise<JsonRpcResponse>((resolve) => {
        sendFn("setKeyboardMacros", { params: { macros: macrosWithSortOrder } }, (response) => {
          resolve(response);
        });
      });

      if (response.error) {
        console.error("Error saving macros:", response.error);
        const errorMessage = typeof response.error.data === 'string'
          ? response.error.data
          : response.error.message || "Failed to save macros";
        throw new Error(errorMessage);
      }

      // Only update the store if the request was successful
      set({ macros: macrosWithSortOrder });
    } catch (error) {
      console.error("Failed to save macros:", error);
      throw error;
    } finally {
      set({ loading: false });
    }
  }
}));