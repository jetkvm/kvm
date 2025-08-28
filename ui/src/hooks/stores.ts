import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

import {
  MAX_STEPS_PER_MACRO,
  MAX_TOTAL_MACROS,
  MAX_KEYS_PER_STEP,
} from "@/constants/macros";

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

export interface UserState {
  user: User | null;
  setUser: (user: User | null) => void;
}

export interface UIState {
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
  setTerminalType: (type: UIState["terminalType"]) => void;
}

export const useUiStore = create<UIState>(set => ({
  terminalType: "none",
  setTerminalType: (type: UIState["terminalType"])  => set({ terminalType: type }),

  sidebarView: null,
  setSidebarView:  (view: AvailableSidebarViews | null) => set({ sidebarView: view }),

  disableVideoFocusTrap: false,
  setDisableVideoFocusTrap: (enabled: boolean) => set({ disableVideoFocusTrap: enabled }),

  isWakeOnLanModalVisible: false,
  setWakeOnLanModalVisibility: (enabled: boolean) => set({ isWakeOnLanModalVisible: enabled }),

  toggleSidebarView: view =>
    set(state => {
      if (state.sidebarView === view) {
        return { sidebarView: null };
      } else {
        return { sidebarView: view };
      }
    }),

  isAttachedVirtualKeyboardVisible: true,
  setAttachedVirtualKeyboardVisibility: (enabled: boolean) =>
    set({ isAttachedVirtualKeyboardVisible: enabled }),
}));

export interface RTCState {
  peerConnection: RTCPeerConnection | null;
  setPeerConnection: (pc: RTCState["peerConnection"]) => void;

  setRpcDataChannel: (channel: RTCDataChannel) => void;
  rpcDataChannel: RTCDataChannel | null;

  peerConnectionState: RTCPeerConnectionState | null;
  setPeerConnectionState: (state: RTCPeerConnectionState) => void;

  transceiver: RTCRtpTransceiver | null;
  setTransceiver: (transceiver: RTCRtpTransceiver) => void;

  mediaStream: MediaStream | null;
  setMediaStream: (stream: MediaStream) => void;

  videoStreamStats: RTCInboundRtpStreamStats | null;
  appendVideoStreamStats: (stats: RTCInboundRtpStreamStats) => void;
  videoStreamStatsHistory: Map<number, RTCInboundRtpStreamStats>;

  isTurnServerInUse: boolean;
  setTurnServerInUse: (inUse: boolean) => void;

  inboundRtpStats: Map<number, RTCInboundRtpStreamStats>;
  appendInboundRtpStats: (stats: RTCInboundRtpStreamStats) => void;
  clearInboundRtpStats: () => void;

  candidatePairStats: Map<number, RTCIceCandidatePairStats>;
  appendCandidatePairStats: (stats: RTCIceCandidatePairStats) => void;
  clearCandidatePairStats: () => void;

  // Remote ICE candidates stat type doesn't exist as of today
  localCandidateStats: Map<number, RTCIceCandidateStats>;
  appendLocalCandidateStats: (stats: RTCIceCandidateStats) => void;

  remoteCandidateStats: Map<number, RTCIceCandidateStats>;
  appendRemoteCandidateStats: (stats: RTCIceCandidateStats) => void;

  // Disk data channel stats type doesn't exist as of today
  diskDataChannelStats: Map<number, RTCDataChannelStats>;
  appendDiskDataChannelStats: (stats: RTCDataChannelStats) => void;

  terminalChannel: RTCDataChannel | null;
  setTerminalChannel: (channel: RTCDataChannel) => void;
}

export const useRTCStore = create<RTCState>(set => ({
  peerConnection: null,
  setPeerConnection: (pc: RTCState["peerConnection"]) => set({ peerConnection: pc }),

  rpcDataChannel: null,
  setRpcDataChannel: (channel: RTCDataChannel) => set({ rpcDataChannel: channel }),

  transceiver: null,
  setTransceiver: (transceiver: RTCRtpTransceiver) => set({ transceiver }),

  peerConnectionState: null,
  setPeerConnectionState: (state: RTCPeerConnectionState) => set({ peerConnectionState: state }),

  mediaStream: null,
  setMediaStream: (stream: MediaStream) => set({ mediaStream: stream }),

  videoStreamStats: null,
  appendVideoStreamStats: (stats: RTCInboundRtpStreamStats) => set({ videoStreamStats: stats }),
  videoStreamStatsHistory: new Map(),

  isTurnServerInUse: false,
  setTurnServerInUse: (inUse: boolean)  => set({ isTurnServerInUse: inUse }),

  inboundRtpStats: new Map(),
  appendInboundRtpStats: (stats: RTCInboundRtpStreamStats) => {
    set(prevState => ({
      inboundRtpStats: appendStatToMap(stats, prevState.inboundRtpStats),
    }));
  },
  clearInboundRtpStats: () => set({ inboundRtpStats: new Map() }),

  candidatePairStats: new Map(),
  appendCandidatePairStats: (stats: RTCIceCandidatePairStats) => {
    set(prevState => ({
      candidatePairStats: appendStatToMap(stats, prevState.candidatePairStats),
    }));
  },
  clearCandidatePairStats: () => set({ candidatePairStats: new Map() }),

  localCandidateStats: new Map(),
  appendLocalCandidateStats: (stats: RTCIceCandidateStats) => {
    set(prevState => ({
      localCandidateStats: appendStatToMap(stats, prevState.localCandidateStats),
    }));
  },

  remoteCandidateStats: new Map(),
  appendRemoteCandidateStats: (stats: RTCIceCandidateStats) => {
    set(prevState => ({
      remoteCandidateStats: appendStatToMap(stats, prevState.remoteCandidateStats),
    }));
  },

  diskDataChannelStats: new Map(),
  appendDiskDataChannelStats: (stats: RTCDataChannelStats) => {
    set(prevState => ({
      diskDataChannelStats: appendStatToMap(stats, prevState.diskDataChannelStats),
    }));
  },

  // Add these new properties to the store implementation
  terminalChannel: null,
  setTerminalChannel: (channel: RTCDataChannel) => set({ terminalChannel: channel }),
}));

export interface MouseMove {
  x: number;
  y: number;
  buttons: number;
}
export interface MouseState {
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
  setMousePosition: (x: number, y: number) => set({ mouseX: x, mouseY: y }),
}));

export type HdmiStates = "ready" | "no_signal" | "no_lock" | "out_of_range" | "connecting";
export type HdmiErrorStates = Extract<VideoState["hdmiState"], "no_signal" | "no_lock" | "out_of_range">

export interface HdmiState {
  ready: boolean;
  error?: HdmiErrorStates;
}

export interface VideoState {
  width: number;
  height: number;
  clientWidth: number;
  clientHeight: number;
  setClientSize: (width: number, height: number) => void;
  setSize: (width: number, height: number) => void;
  hdmiState: HdmiStates;
  setHdmiState: (state: {
    ready: boolean;
    error?: HdmiErrorStates;
  }) => void;
}

export const useVideoStore = create<VideoState>(set => ({
  width: 0,
  height: 0,

  clientWidth: 0,
  clientHeight: 0,

  // The video element's client size
  setClientSize: (clientWidth: number, clientHeight: number) => set({ clientWidth, clientHeight }),

  // Resolution
  setSize: (width: number, height: number) => set({ width, height }),

  hdmiState: "connecting",
  setHdmiState: (state: HdmiState) => {
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

export interface BacklightSettings {
  max_brightness: number;
  dim_after: number;
  off_after: number;
}

export interface SettingsState {
  isCursorHidden: boolean;
  setCursorVisibility: (enabled: boolean) => void;

  mouseMode: string;
  setMouseMode: (mode: string) => void;

  debugMode: boolean;
  setDebugMode: (enabled: boolean) => void;

  // Add new developer mode state
  developerMode: boolean;
  setDeveloperMode: (enabled: boolean) => void;

  displayRotation: string;
  setDisplayRotation: (rotation: string) => void;

  backlightSettings: BacklightSettings;
  setBacklightSettings: (settings: BacklightSettings) => void;

  keyboardLayout: string;
  setKeyboardLayout: (layout: string) => void;

  scrollThrottling: number;
  setScrollThrottling: (value: number) => void;

  showPressedKeys: boolean;
  setShowPressedKeys: (show: boolean) => void;

  // Video enhancement settings
  videoSaturation: number;
  setVideoSaturation: (value: number) => void;
  videoBrightness: number;
  setVideoBrightness: (value: number) => void;
  videoContrast: number;
  setVideoContrast: (value: number) => void;
}

export const useSettingsStore = create(
  persist<SettingsState>(
    set => ({
      isCursorHidden: false,
      setCursorVisibility: (enabled: boolean) => set({ isCursorHidden: enabled }),

      mouseMode: "absolute",
      setMouseMode: (mode: string) => set({ mouseMode: mode }),

      debugMode: import.meta.env.DEV,
      setDebugMode: (enabled: boolean) => set({ debugMode: enabled }),

      // Add developer mode with default value
      developerMode: false,
      setDeveloperMode: (enabled: boolean) => set({ developerMode: enabled }),

      displayRotation: "270",
      setDisplayRotation: (rotation: string) => set({ displayRotation: rotation }),

      backlightSettings: {
        max_brightness: 100,
        dim_after: 10000,
        off_after: 50000,
      },
      setBacklightSettings: (settings: BacklightSettings) =>
        set({ backlightSettings: settings }),

      keyboardLayout: "en-US",
      setKeyboardLayout: (layout: string) => set({ keyboardLayout: layout }),

      scrollThrottling: 0,
      setScrollThrottling: (value: number) => set({ scrollThrottling: value }),

      showPressedKeys: true,
      setShowPressedKeys: (show: boolean) => set({ showPressedKeys: show }),

      // Video enhancement settings with default values (1.0 = normal)
      videoSaturation: 1.0,
      setVideoSaturation: (value: number) => set({ videoSaturation: value }),
      videoBrightness: 1.0,
      setVideoBrightness: (value: number) => set({ videoBrightness: value }),
      videoContrast: 1.0,
      setVideoContrast: (value: number) => set({ videoContrast: value }),
    }),
    {
      name: "settings",
      storage: createJSONStorage(() => localStorage),
    },
  ),
);

export interface RemoteVirtualMediaState {
  source: "HTTP" | "Storage" | null;
  mode: "CDROM" | "Disk" | null;
  filename: string | null;
  url: string | null;
  path: string | null;
  size: number | null;
}

export interface MountMediaState {
  remoteVirtualMediaState: RemoteVirtualMediaState | null;
  setRemoteVirtualMediaState: (state: MountMediaState["remoteVirtualMediaState"]) => void;

  modalView: "mode" | "url" | "device" | "upload" | "error" | null;
  setModalView: (view: MountMediaState["modalView"]) => void;

  isMountMediaDialogOpen: boolean;
  setIsMountMediaDialogOpen: (isOpen: MountMediaState["isMountMediaDialogOpen"]) => void;

  uploadedFiles: { name: string; size: string; uploadedAt: string }[];
  addUploadedFile: (file: { name: string; size: string; uploadedAt: string }) => void;

  errorMessage: string | null;
  setErrorMessage: (message: string | null) => void;
}

export const useMountMediaStore = create<MountMediaState>(set => ({
  remoteVirtualMediaState: null,
  setRemoteVirtualMediaState: (state: MountMediaState["remoteVirtualMediaState"]) => set({ remoteVirtualMediaState: state }),

  modalView: "mode",
  setModalView: (view: MountMediaState["modalView"]) => set({ modalView: view }),

  isMountMediaDialogOpen: false,
  setIsMountMediaDialogOpen: (isOpen: MountMediaState["isMountMediaDialogOpen"]) => set({ isMountMediaDialogOpen: isOpen }),

  uploadedFiles: [],
  addUploadedFile: (file: { name: string; size: string; uploadedAt: string }) =>
    set(state => ({ uploadedFiles: [...state.uploadedFiles, file] })),

  errorMessage: null,
  setErrorMessage: (message: string | null) => set({ errorMessage: message }),
}));

export interface KeyboardLedState {
  num_lock: boolean;
  caps_lock: boolean;
  scroll_lock: boolean;
  compose: boolean;
  kana: boolean;
  shift: boolean; // Optional, as not all keyboards have a shift LED
};

export const hidKeyBufferSize = 6;
export const hidErrorRollOver = 0x01;

export interface KeysDownState {
	modifier: number;
	keys: number[];
}

export type USBStates = 
  | "configured"
  | "attached"
  | "not attached"
  | "suspended"
  | "addressed";

export interface HidState {
  keyboardLedState: KeyboardLedState;
  setKeyboardLedState: (state: KeyboardLedState) => void;

  keysDownState: KeysDownState;
  setKeysDownState: (state: KeysDownState) => void;

  keyPressReportApiAvailable: boolean;
  setkeyPressReportApiAvailable: (available: boolean) => void;

  isVirtualKeyboardEnabled: boolean;
  setVirtualKeyboardEnabled: (enabled: boolean) => void;

  isPasteModeEnabled: boolean;
  setPasteModeEnabled: (enabled: boolean) => void;

  usbState: USBStates;
  setUsbState: (state: USBStates) => void;
}

export const useHidStore = create<HidState>(set => ({
  keyboardLedState: {} as KeyboardLedState,
  setKeyboardLedState: (ledState: KeyboardLedState): void => set({ keyboardLedState: ledState }),

  keysDownState: { modifier: 0, keys: [0,0,0,0,0,0] } as KeysDownState,
  setKeysDownState: (state: KeysDownState): void => set({ keysDownState: state }),

  keyPressReportApiAvailable: true,
  setkeyPressReportApiAvailable: (available: boolean) => set({ keyPressReportApiAvailable: available }),

  isVirtualKeyboardEnabled: false,
  setVirtualKeyboardEnabled: (enabled: boolean): void => set({ isVirtualKeyboardEnabled: enabled }),

  isPasteModeEnabled: false,
  setPasteModeEnabled: (enabled: boolean): void => set({ isPasteModeEnabled: enabled }),

  // Add these new properties for USB state
  usbState: "not attached",
  setUsbState: (state: USBStates) => set({ usbState: state }),
}));

export const useUserStore = create<UserState>(set => ({
  user: null,
  setUser: user => set({ user }),
}));

export type UpdateModalViews =
  | "loading"
  | "updating"
  | "upToDate"
  | "updateAvailable"
  | "updateCompleted"
  | "error";

export interface OtaState {
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

export interface UpdateState {
  isUpdatePending: boolean;
  setIsUpdatePending: (isPending: boolean) => void;
  updateDialogHasBeenMinimized: boolean;
  otaState: OtaState;
  setOtaState: (state: OtaState) => void;
  setUpdateDialogHasBeenMinimized: (hasBeenMinimized: boolean) => void;
  modalView: UpdateModalViews
  setModalView: (view: UpdateModalViews) => void;
  setUpdateErrorMessage: (errorMessage: string) => void;
  updateErrorMessage: string | null;
}

export const useUpdateStore = create<UpdateState>(set => ({
  isUpdatePending: false,
  setIsUpdatePending: (isPending: boolean) => set({ isUpdatePending: isPending }),

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
  setUpdateDialogHasBeenMinimized: (hasBeenMinimized: boolean) =>
    set({ updateDialogHasBeenMinimized: hasBeenMinimized }),
  modalView: "loading",
  setModalView: (view: UpdateModalViews) => set({ modalView: view }),
  updateErrorMessage: null,
  setUpdateErrorMessage: (errorMessage: string) => set({ updateErrorMessage: errorMessage }),
}));

export type UsbConfigModalViews =
  | "updateUsbConfig"
  | "updateUsbConfigSuccess";

export interface UsbConfigModalState {
  modalView: UsbConfigModalViews ;
  errorMessage: string | null;
  setModalView: (view: UsbConfigModalViews) => void;
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
  setModalView:  (view: UsbConfigModalViews) => set({ modalView: view }),
  setErrorMessage: (message: string | null) => set({ errorMessage: message }),
}));

export type LocalAuthModalViews =
  | "createPassword"
  | "deletePassword"
  | "updatePassword"
  | "creationSuccess"
  | "deleteSuccess"
  | "updateSuccess";

export interface LocalAuthModalState {
  modalView:LocalAuthModalViews;
  setModalView: (view:LocalAuthModalViews) => void;
}

export const useLocalAuthModalStore = create<LocalAuthModalState>(set => ({
  modalView: "createPassword",
  setModalView: (view: LocalAuthModalViews)  => set({ modalView: view }),
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

  setAppVersion: (version: string) => set({ appVersion: version }),
  setSystemVersion: (version: string) => set({ systemVersion: version }),
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

export type IPv6Mode =
  | "disabled"
  | "slaac"
  | "dhcpv6"
  | "slaac_and_dhcpv6"
  | "static"
  | "link_local"
  | "unknown";
export type IPv4Mode = "disabled" | "static" | "dhcp" | "unknown";
export type LLDPMode = "disabled" | "basic" | "all" | "unknown";
export type mDNSMode = "disabled" | "auto" | "ipv4_only" | "ipv6_only" | "unknown";
export type TimeSyncMode =
  | "ntp_only"
  | "ntp_and_http"
  | "http_only"
  | "custom"
  | "unknown";

export interface NetworkSettings {
  hostname: string;
  domain: string;
  http_proxy: string;
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
  },
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
  sendFn:
    | ((
        method: string,
        params: unknown,
        callback?: ((resp: JsonRpcResponse) => void) | undefined,
      ) => void)
    | null;
  setSendFn: (
    sendFn: (
      method: string,
      params: unknown,
      callback?: ((resp: JsonRpcResponse) => void) | undefined,
    ) => void,
  ) => void;
}

export const generateMacroId = () => {
  return Math.random().toString(36).substring(2, 9);
};

export const useMacrosStore = create<MacrosState>((set, get) => ({
  macros: [],
  loading: false,
  initialized: false,
  sendFn: null,

  setSendFn: sendFn => {
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
        sendFn("getKeyboardMacros", {}, (response: JsonRpcResponse) => {
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
            initialized: true,
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
        console.error(
          `Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`,
        );
        throw new Error(
          `Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`,
        );
      }

      for (let i = 0; i < macro.steps.length; i++) {
        const step = macro.steps[i];
        if (step.keys && step.keys.length > MAX_KEYS_PER_STEP) {
          console.error(
            `Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`,
          );
          throw new Error(
            `Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`,
          );
        }
      }
    }

    set({ loading: true });

    try {
      const macrosWithSortOrder = macros.map((macro, index) => ({
        ...macro,
        sortOrder: macro.sortOrder !== undefined ? macro.sortOrder : index,
      }));

      const response = await new Promise<JsonRpcResponse>(resolve => {
        sendFn(
          "setKeyboardMacros",
          { params: { macros: macrosWithSortOrder } },
          (response: JsonRpcResponse) => {
            resolve(response);
          },
        );
      });

      if (response.error) {
        console.error("Error saving macros:", response.error);
        const errorMessage =
          typeof response.error.data === "string"
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
