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

export type PostRebootAction = {
  healthCheck: string;
  redirectTo: string;
} | null;

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
export type AvailableTerminalTypes = "kvm" | "serial" | "cdcacm" | "guest" | "none";

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

  rebootState: { isRebooting: boolean; postRebootAction: PostRebootAction } | null;
  setRebootState: (
    state: { isRebooting: boolean; postRebootAction: PostRebootAction } | null,
  ) => void;

  isOcrMode: boolean;
  setOcrMode: (enabled: boolean) => void;

  // Text captured from the browser clipboard that the paste popover should
  // open with; consumed by the popover once shown.
  pendingPasteText: string | null;
  setPendingPasteText: (text: string | null) => void;

  usbSerialConsoleEnabled: boolean;
  setUsbSerialConsoleEnabled: (enabled: boolean) => void;

  isEmbedMode: boolean;
  setEmbedMode: (enabled: boolean) => void;
}

export const useUiStore = create<UIState>(set => ({
  terminalType: "none",
  setTerminalType: (type: UIState["terminalType"]) => set({ terminalType: type }),

  sidebarView: null,
  setSidebarView: (view: AvailableSidebarViews | null) => set({ sidebarView: view }),

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

  rebootState: null,
  setRebootState: state => set({ rebootState: state }),

  isOcrMode: false,
  setOcrMode: (enabled: boolean) => set({ isOcrMode: enabled }),

  pendingPasteText: null,
  setPendingPasteText: (text: string | null) => set({ pendingPasteText: text }),

  usbSerialConsoleEnabled: false,
  setUsbSerialConsoleEnabled: (enabled: boolean) => set({ usbSerialConsoleEnabled: enabled }),

  isEmbedMode: false,
  setEmbedMode: (enabled: boolean) => set({ isEmbedMode: enabled }),
}));

export interface RTCState {
  peerConnection: RTCPeerConnection | null;
  setPeerConnection: (pc: RTCState["peerConnection"]) => void;

  setRpcDataChannel: (channel: RTCDataChannel | null) => void;
  rpcDataChannel: RTCDataChannel | null;

  hidRpcDisabled: boolean;
  setHidRpcDisabled: (disabled: boolean) => void;

  rpcHidProtocolVersion: number | null;
  setRpcHidProtocolVersion: (version: number | null) => void;

  rpcHidChannel: RTCDataChannel | null;
  setRpcHidChannel: (channel: RTCDataChannel | null) => void;

  rpcHidUnreliableChannel: RTCDataChannel | null;
  setRpcHidUnreliableChannel: (channel: RTCDataChannel | null) => void;

  rpcHidUnreliableNonOrderedChannel: RTCDataChannel | null;
  setRpcHidUnreliableNonOrderedChannel: (channel: RTCDataChannel | null) => void;

  peerConnectionState: RTCPeerConnectionState | null;
  setPeerConnectionState: (state: RTCPeerConnectionState) => void;

  transceiver: RTCRtpTransceiver | null;
  setTransceiver: (transceiver: RTCRtpTransceiver) => void;

  mediaStream: MediaStream | null;
  setMediaStream: (stream: MediaStream | null) => void;
  // MediaStream.addTrack() does not fire `addtrack`, so consumers that
  // need to react to a track joining the canonical stream watch this bump.
  mediaStreamTrackVersion: number;
  bumpMediaStreamTrackVersion: () => void;

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
  setTerminalChannel: (channel: RTCDataChannel | null) => void;
}

export const useRTCStore = create<RTCState>(set => ({
  peerConnection: null,
  setPeerConnection: (pc: RTCState["peerConnection"]) => set({ peerConnection: pc }),

  rpcDataChannel: null,
  setRpcDataChannel: channel => set({ rpcDataChannel: channel }),

  hidRpcDisabled: false,
  setHidRpcDisabled: disabled => set({ hidRpcDisabled: disabled }),

  rpcHidProtocolVersion: null,
  setRpcHidProtocolVersion: version => set({ rpcHidProtocolVersion: version }),

  rpcHidChannel: null,
  setRpcHidChannel: channel => set({ rpcHidChannel: channel }),

  rpcHidUnreliableChannel: null,
  setRpcHidUnreliableChannel: channel => set({ rpcHidUnreliableChannel: channel }),

  rpcHidUnreliableNonOrderedChannel: null,
  setRpcHidUnreliableNonOrderedChannel: channel =>
    set({ rpcHidUnreliableNonOrderedChannel: channel }),

  transceiver: null,
  setTransceiver: transceiver => set({ transceiver }),

  peerConnectionState: null,
  setPeerConnectionState: state => set({ peerConnectionState: state }),

  mediaStream: null,
  setMediaStream: stream => set({ mediaStream: stream, mediaStreamTrackVersion: 0 }),
  mediaStreamTrackVersion: 0,
  bumpMediaStreamTrackVersion: () =>
    set(s => ({ mediaStreamTrackVersion: s.mediaStreamTrackVersion + 1 })),

  videoStreamStats: null,
  appendVideoStreamStats: stats => set({ videoStreamStats: stats }),
  videoStreamStatsHistory: new Map(),

  isTurnServerInUse: false,
  setTurnServerInUse: inUse => set({ isTurnServerInUse: inUse }),

  inboundRtpStats: new Map(),
  appendInboundRtpStats: stats => {
    set(prevState => ({
      inboundRtpStats: appendStatToMap(stats, prevState.inboundRtpStats),
    }));
  },
  clearInboundRtpStats: () => set({ inboundRtpStats: new Map() }),

  candidatePairStats: new Map(),
  appendCandidatePairStats: stats => {
    set(prevState => ({
      candidatePairStats: appendStatToMap(stats, prevState.candidatePairStats),
    }));
  },
  clearCandidatePairStats: () => set({ candidatePairStats: new Map() }),

  localCandidateStats: new Map(),
  appendLocalCandidateStats: stats => {
    set(prevState => ({
      localCandidateStats: appendStatToMap(stats, prevState.localCandidateStats),
    }));
  },

  remoteCandidateStats: new Map(),
  appendRemoteCandidateStats: stats => {
    set(prevState => ({
      remoteCandidateStats: appendStatToMap(stats, prevState.remoteCandidateStats),
    }));
  },

  diskDataChannelStats: new Map(),
  appendDiskDataChannelStats: stats => {
    set(prevState => ({
      diskDataChannelStats: appendStatToMap(stats, prevState.diskDataChannelStats),
    }));
  },

  // Add these new properties to the store implementation
  terminalChannel: null,
  setTerminalChannel: channel => set({ terminalChannel: channel }),
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
  setMouseMove: move => set({ mouseMove: move }),
  setMousePosition: (x, y) => set({ mouseX: x, mouseY: y }),
}));

export type HdmiStates = "ready" | "no_signal" | "no_lock" | "out_of_range" | "connecting";
export type HdmiErrorStates = Extract<
  VideoState["hdmiState"],
  "no_signal" | "no_lock" | "out_of_range"
>;

export interface HdmiState {
  ready: boolean;
  error?: HdmiErrorStates;
}

export type VideoSource = "physical" | "guest" | "switching_to_physical" | "switching_to_guest";

// Clipboard access advertised by a connected OS Services sender.
export interface GuestClipboardState {
  read: boolean;
  write: boolean;
  os: "windows" | "macos" | "linux" | "unknown";
}

// Live OS Services state as the KVM reports it in every video state.
export interface OsServicesStatus {
  sender?: { version: string; displays: string[] };
  connected: boolean;
  video: boolean;
  // The sender registered its video service; without it the video setting
  // has nothing to act on.
  videoRegistered: boolean;
  // Why the sender's video service faulted, while it is faulted.
  videoError?: string;
  clipboard: boolean;
  // The sender waits for its user to approve this KVM's key.
  approvalPending: boolean;
  // This KVM's key, as the sender's approval dialog shows it; empty until
  // the first sender connects.
  fingerprint: string;
}

const noOsServices: OsServicesStatus = {
  connected: false,
  video: false,
  videoRegistered: false,
  clipboard: false,
  approvalPending: false,
  fingerprint: "",
};

// The getVideoState reply and every videoInputState event. The OS Services
// fields come only from a device with the os_services capability.
export interface RpcVideoState extends HdmiState {
  source?: VideoSource;
  osServices?: OsServicesStatus;
  guestClipboard?: GuestClipboardState;
  guestTerminal?: boolean;
  guestFile?: GuestFileState;
  guestUpdate?: GuestUpdateState;
}

// Desired state persisted by JetKVM.
export interface OsServicesSettings {
  adaptation?: string;
  terminal_enabled: boolean;
  files_enabled: boolean;
  capture_display: string;
  video_enabled: boolean;
  resolution: string;
  display_mode: "mirror" | "extend";
  clipboard_enabled: boolean;
}

export interface OsServicesSettingsEvent {
  sequence: number;
  settings: OsServicesSettings;
}

// File reception advertised by a connected OS Services sender: files
// dropped on the video are written to the guest's Downloads folder, with a home-folder fallback.
export interface GuestFileState {
  receive: boolean;
}

// The newest OS Services release the KVM firmware installs, and the update
// of the connected sender to it. Absent while no release is known.
export interface GuestUpdateState {
  version: string;
  // The connected sender is older than the release.
  available: boolean;
  // The connected sender can install the release's package; otherwise the
  // installer ISO is the way to update.
  installable: boolean;
  // Auto-update is on: the KVM installs the update itself once no browser
  // is connected, so the status card does not offer it. The settings page
  // still can.
  automatic: boolean;
  phase?: "downloading" | "sending" | "installing" | "done" | "failed";
  sent?: number;
  size?: number;
  error?: string;
}

export interface VideoState {
  width: number;
  height: number;
  clientWidth: number;
  clientHeight: number;
  setClientSize: (width: number, height: number) => void;
  setSize: (width: number, height: number) => void;
  hdmiState: HdmiStates;
  videoSource: VideoSource;
  // Decided by the KVM and shown as-is in the info bar.
  osServices: OsServicesStatus;
  guestClipboard: GuestClipboardState | null;
  setGuestClipboard: (state: GuestClipboardState | null) => void;
  // Every reader of a video state payload applies it as one store update.
  applyVideoState: (state: RpcVideoState) => void;
  osServicesSettings: OsServicesSettings | null;
  osServicesSettingsSequence: number;
  // Announcements can arrive out of order; one older than the last applied
  // is dropped. The sequence belongs to one backend process, so a new RPC
  // connection starts it over.
  setOsServicesSettings: (event: OsServicesSettingsEvent) => void;
  resetOsServicesSettings: () => void;

  // True while a connected OS Services sender offers a shell.
  guestTerminal: boolean;
  guestFile: GuestFileState | null;
  guestUpdate: GuestUpdateState | null;
  videoElement: HTMLVideoElement | null;
  setVideoElement: (element: HTMLVideoElement | null) => void;
  containerElement: HTMLElement | null;
  setContainerElement: (element: HTMLElement | null) => void;
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
  videoSource: "physical",
  osServices: noOsServices,
  guestClipboard: null,
  setGuestClipboard: (guestClipboard: GuestClipboardState | null) => set({ guestClipboard }),
  guestTerminal: false,
  guestFile: null,
  guestUpdate: null,

  applyVideoState: (state: RpcVideoState) =>
    set(store => ({
      hdmiState: state.ready ? "ready" : (state.error ?? "connecting"),
      videoSource: state.source ?? store.videoSource,
      // A device without the field (no OS Services) has no OS Services,
      // and the store must not keep the previous device's.
      osServices: state.osServices ?? noOsServices,
      guestClipboard: state.guestClipboard ?? null,
      guestTerminal: state.guestTerminal ?? false,
      guestFile: state.guestFile ?? null,
      guestUpdate: state.guestUpdate ?? null,
    })),

  osServicesSettings: null,
  osServicesSettingsSequence: 0,
  setOsServicesSettings: (event: OsServicesSettingsEvent) =>
    set(state =>
      event.sequence > state.osServicesSettingsSequence
        ? {
            osServicesSettings: event.settings,
            osServicesSettingsSequence: event.sequence,
          }
        : {},
    ),
  resetOsServicesSettings: () => set({ osServicesSettings: null, osServicesSettingsSequence: 0 }),

  videoElement: null,
  setVideoElement: (element: HTMLVideoElement | null) => set({ videoElement: element }),
  containerElement: null,
  setContainerElement: (element: HTMLElement | null) => set({ containerElement: element }),
}));

export interface BacklightSettings {
  max_brightness: number;
  dim_after: number;
  off_after: number;
}

export type VideoScaling = "fit" | "actual";

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

  invertScroll: boolean;
  setInvertScroll: (value: boolean) => void;

  showPressedKeys: boolean;
  setShowPressedKeys: (show: boolean) => void;

  // Video enhancement settings
  videoSaturation: number;
  setVideoSaturation: (value: number) => void;

  videoBrightness: number;
  setVideoBrightness: (value: number) => void;

  videoContrast: number;
  setVideoContrast: (value: number) => void;

  videoScaling: VideoScaling;
  setVideoScaling: (scaling: VideoScaling) => void;

  hideHeaderBar: boolean;
  setHideHeaderBar: (hide: boolean) => void;

  hideStatusBar: boolean;
  setHideStatusBar: (hide: boolean) => void;
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
      setBacklightSettings: (settings: BacklightSettings) => set({ backlightSettings: settings }),

      keyboardLayout: "en-US",
      setKeyboardLayout: (layout: string) => set({ keyboardLayout: layout }),

      scrollThrottling: 0,
      setScrollThrottling: (value: number) => set({ scrollThrottling: value }),

      invertScroll: false,
      setInvertScroll: (value: boolean) => set({ invertScroll: value }),

      showPressedKeys: true,
      setShowPressedKeys: (show: boolean) => set({ showPressedKeys: show }),

      // Video enhancement settings with default values (1.0 = normal)
      videoSaturation: 1.0,
      setVideoSaturation: (value: number) => set({ videoSaturation: value }),

      videoBrightness: 1.0,
      setVideoBrightness: (value: number) => set({ videoBrightness: value }),

      videoContrast: 1.0,
      setVideoContrast: (value: number) => set({ videoContrast: value }),

      videoScaling: "fit",
      setVideoScaling: (scaling: VideoScaling) => set({ videoScaling: scaling }),

      hideHeaderBar: false,
      setHideHeaderBar: (hide: boolean) => set({ hideHeaderBar: hide }),

      hideStatusBar: false,
      setHideStatusBar: (hide: boolean) => set({ hideStatusBar: hide }),
    }),
    {
      name: "settings",
      storage: createJSONStorage(() => localStorage),
    },
  ),
);

export interface RemoteVirtualMediaState {
  source: "HTTP" | "Storage" | "Builtin" | null;
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
  setRemoteVirtualMediaState: (state: MountMediaState["remoteVirtualMediaState"]) =>
    set({ remoteVirtualMediaState: state }),

  modalView: "mode",
  setModalView: (view: MountMediaState["modalView"]) => set({ modalView: view }),

  isMountMediaDialogOpen: false,
  setIsMountMediaDialogOpen: (isOpen: MountMediaState["isMountMediaDialogOpen"]) =>
    set({ isMountMediaDialogOpen: isOpen }),

  uploadedFiles: [],
  addUploadedFile: (file: { name: string; size: string; uploadedAt: string }) =>
    set(state => ({ uploadedFiles: [...state.uploadedFiles, file] })),

  errorMessage: null,
  setErrorMessage: (message: string | null) => set({ errorMessage: message }),
}));

// downloadToStorage on a device with removable storage: one file streamed
// from a URL onto its card. videoPaused is true while the transfer holds
// video off.
export interface StorageDownloadState {
  state: "idle" | "downloading" | "done" | "error";
  filename: string;
  bytes: number;
  total: number;
  reused: boolean;
  videoPaused: boolean;
  error: string;
}

export interface StorageDownloadStore {
  download: StorageDownloadState | null;
  setDownload: (download: StorageDownloadState | null) => void;
}

export const useStorageDownloadStore = create<StorageDownloadStore>(set => ({
  download: null,
  setDownload: download => set({ download }),
}));

export interface KeyboardLedState {
  num_lock: boolean;
  caps_lock: boolean;
  scroll_lock: boolean;
  compose: boolean;
  kana: boolean;
  shift: boolean; // Optional, as not all keyboards have a shift LED
}

export const hidKeyBufferSize = 6;
export const hidErrorRollOver = 0x01;

export interface KeysDownState {
  modifier: number;
  keys: number[];
}

export type USBStates = "configured" | "attached" | "not attached" | "suspended" | "addressed";

export interface HidState {
  keyboardLedState: KeyboardLedState;
  setKeyboardLedState: (state: KeyboardLedState) => void;

  keysDownState: KeysDownState;
  setKeysDownState: (state: KeysDownState) => void;

  isVirtualKeyboardEnabled: boolean;
  setVirtualKeyboardEnabled: (enabled: boolean) => void;

  isPasteInProgress: boolean;
  setPasteModeEnabled: (enabled: boolean) => void;

  usbState: USBStates;
  setUsbState: (state: USBStates) => void;
}

export const useHidStore = create<HidState>(set => ({
  keyboardLedState: {
    num_lock: false,
    caps_lock: false,
    scroll_lock: false,
    compose: false,
    kana: false,
    shift: false,
  } as KeyboardLedState,
  setKeyboardLedState: (ledState: KeyboardLedState): void => set({ keyboardLedState: ledState }),

  keysDownState: { modifier: 0, keys: [0, 0, 0, 0, 0, 0] } as KeysDownState,
  setKeysDownState: (state: KeysDownState): void => set({ keysDownState: state }),

  isVirtualKeyboardEnabled: false,
  setVirtualKeyboardEnabled: (enabled: boolean): void => set({ isVirtualKeyboardEnabled: enabled }),

  isPasteInProgress: false,
  setPasteModeEnabled: (enabled: boolean): void => set({ isPasteInProgress: enabled }),

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
}

export interface UpdateState {
  isUpdatePending: boolean;
  setIsUpdatePending: (isPending: boolean) => void;

  updateDialogHasBeenMinimized: boolean;
  setUpdateDialogHasBeenMinimized: (hasBeenMinimized: boolean) => void;

  otaState: OtaState;
  setOtaState: (state: OtaState) => void;

  modalView: UpdateModalViews;
  setModalView: (view: UpdateModalViews) => void;

  updateErrorMessage: string | null;
  setUpdateErrorMessage: (errorMessage: string) => void;

  shouldReload: boolean;
  setShouldReload: (reloadRequired: boolean) => void;
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

  shouldReload: false,
  setShouldReload: (reloadRequired: boolean) => set({ shouldReload: reloadRequired }),
}));

export type UsbConfigModalViews = "updateUsbConfig" | "updateUsbConfigSuccess";

export interface UsbConfigModalState {
  modalView: UsbConfigModalViews;
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
  setModalView: (view: UsbConfigModalViews) => set({ modalView: view }),
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
  modalView: LocalAuthModalViews;
  setModalView: (view: LocalAuthModalViews) => void;
}

export const useLocalAuthModalStore = create<LocalAuthModalState>(set => ({
  modalView: "createPassword",
  setModalView: (view: LocalAuthModalViews) => set({ modalView: view }),
}));

export interface DeviceState {
  appVersion: string | null;
  systemVersion: string | null;
  // null until the device reports them on this RPC channel.
  capabilities: Set<string> | null;

  setAppVersion: (version: string) => void;
  setSystemVersion: (version: string) => void;
  setCapabilities: (capabilities: string[] | null) => void;
}

// Optional device features reported by getDeviceCapabilities.
export type Capability =
  | "shell"
  | "extensions"
  | "usb_serial"
  | "custom_edid"
  | "upload_channel"
  | "video_during_update" // video keeps streaming while an update installs
  | "removable_storage" // images live on a card that can be missing or unformatted
  | "wifi" // Wi-Fi networks and frequency band
  | "http_proxy" // network settings the device can change
  | "domain"
  | "mdns"
  | "ipv6"
  | "tls" // HTTPS listener with a selectable certificate
  | "local_loopback" // web UI can be limited to the loopback interface
  | "log_level" // default log level can be changed
  | "tailscale" // Tailscale client
  | "os_services"; // USB link to the OS Services sender on the remote host (clipboard, file drop, shell)

export const useDeviceStore = create<DeviceState>(set => ({
  appVersion: null,
  systemVersion: null,
  capabilities: null,

  setAppVersion: (version: string) => set({ appVersion: version }),
  setSystemVersion: (version: string) => set({ systemVersion: version }),
  setCapabilities: capabilities => set({ capabilities: capabilities && new Set(capabilities) }),
}));

// Whether the connected device reported a capability in getDeviceCapabilities.
export const useCapability = (name: Capability) =>
  useDeviceStore(state => state.capabilities?.has(name) ?? false);

export interface TerminalState {
  terminator: string | null;

  setTerminator: (version: string) => void;
}

export const useTerminalStore = create<TerminalState>(set => ({
  terminator: null,

  setTerminator: (version: string) => set({ terminator: version }),
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
  dns_servers?: string[];
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
  lease?: number; // Go time.Duration, in nanoseconds
  lease_expiry?: Date;
  dhcp_type?: string;
  server_id?: string;
  message?: string;
  tftp?: string;
  bootfile?: string;
  dhcp_client?: string;
}

export interface IPv6Address {
  address: string;
  prefix: string;
  valid_lifetime: string;
  preferred_lifetime: string;
  scope: string;
  flags: number;
  flag_secondary?: boolean;
  flag_permanent?: boolean;
  flag_temporary?: boolean;
  flag_stable_privacy?: boolean;
  flag_deprecated?: boolean;
  flag_optimistic?: boolean;
  flag_dad_failed?: boolean;
  flag_tentative?: boolean;
}

export interface PublicIP {
  ip: string;
  last_updated: Date;
}

export interface TailscalePeer {
  hostName: string;
  dnsName: string;
  tailscaleIPs: string[];
  online: boolean;
  os: string;
}

export interface TailscaleStatus {
  installed: boolean;
  running: boolean;
  backendState?: string;
  authURL?: string;
  controlURL?: string;
  self?: TailscalePeer;
  health?: string[];
}

export interface NetworkState {
  interface_name?: string;
  mac_address?: string;
  ipv4?: string;
  ipv4_addresses?: string[];
  ipv6?: string;
  ipv6_addresses?: IPv6Address[];
  ipv6_link_local?: string;
  ipv6_gateway?: string;
  dhcp_lease?: DhcpLease;
  hostname?: string;

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
export type TimeSyncMode = "ntp_only" | "ntp_and_http" | "http_only" | "custom" | "unknown";

export interface IPv4StaticConfig {
  address: string;
  netmask: string;
  gateway: string;
  dns: string[];
}

export interface IPv6StaticConfig {
  prefix: string;
  gateway: string;
  dns: string[];
}

export interface NetworkSettings {
  dhcp_client: string;
  hostname: string | null;
  domain: string | null;
  http_proxy: string | null;
  ipv4_mode: IPv4Mode;
  ipv4_static?: IPv4StaticConfig;
  ipv6_mode: IPv6Mode;
  ipv6_static?: IPv6StaticConfig;
  lldp_mode: LLDPMode;
  lldp_tx_tlvs: string[];
  mdns_mode: mDNSMode;
  time_sync_mode: TimeSyncMode;
  time_sync_ordering?: string[];
  time_sync_disable_fallback?: boolean;
  time_sync_parallel?: number;
  time_sync_ntp_servers?: string[];
  time_sync_http_urls?: string[];
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
    | ((method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => void)
    | null;
  setSendFn: (
    sendFn: (method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => void,
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
  },
}));

export interface FailsafeModeState {
  isFailsafeMode: boolean;
  reason: string; // "video", "network", etc.
  setFailsafeMode: (active: boolean, reason: string) => void;
}

export const useFailsafeModeStore = create<FailsafeModeState>(set => ({
  isFailsafeMode: false,
  reason: "",
  setFailsafeMode: (active, reason) => set({ isFailsafeMode: active, reason }),
}));
