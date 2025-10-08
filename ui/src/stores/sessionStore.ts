import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";

export type SessionMode = "primary" | "observer" | "queued" | "pending";

export interface SessionInfo {
  id: string;
  mode: SessionMode;
  source: "local" | "cloud";
  identity?: string;
  nickname?: string;
  createdAt: string;
  lastActive: string;
}

export interface SessionState {
  // Current session info
  currentSessionId: string | null;
  currentMode: SessionMode | null;

  // All active sessions
  sessions: SessionInfo[];

  // UI state
  isRequestingPrimary: boolean;
  sessionError: string | null;
  rejectionCount: number;

  // Actions
  setCurrentSession: (id: string, mode: SessionMode) => void;
  setSessions: (sessions: SessionInfo[]) => void;
  setRequestingPrimary: (requesting: boolean) => void;
  setSessionError: (error: string | null) => void;
  updateSessionMode: (mode: SessionMode) => void;
  clearSession: () => void;
  incrementRejectionCount: () => number;
  resetRejectionCount: () => void;

  // Computed getters
  isPrimary: () => boolean;
  isObserver: () => boolean;
  isQueued: () => boolean;
  isPending: () => boolean;
  canRequestPrimary: () => boolean;
  getPrimarySession: () => SessionInfo | undefined;
  getQueuePosition: () => number;
}

export const useSessionStore = create<SessionState>()(
  persist(
    (set, get) => ({
  // Initial state
  currentSessionId: null,
  currentMode: null,
  sessions: [],
  isRequestingPrimary: false,
  sessionError: null,
  rejectionCount: 0,

  // Actions
  setCurrentSession: (id: string, mode: SessionMode) => {
    set({
      currentSessionId: id,
      currentMode: mode,
      sessionError: null
    });
  },

  setSessions: (sessions: SessionInfo[]) => {
    set({ sessions });
  },

  setRequestingPrimary: (requesting: boolean) => {
    set({ isRequestingPrimary: requesting });
  },

  setSessionError: (error: string | null) => {
    set({ sessionError: error });
  },

  updateSessionMode: (mode: SessionMode) => {
    set({ currentMode: mode });
  },

  clearSession: () => {
    set({
      currentSessionId: null,
      currentMode: null,
      sessions: [],
      sessionError: null,
      isRequestingPrimary: false,
      rejectionCount: 0
    });
  },

  incrementRejectionCount: () => {
    const newCount = get().rejectionCount + 1;
    set({ rejectionCount: newCount });
    return newCount;
  },

  resetRejectionCount: () => {
    set({ rejectionCount: 0 });
  },

  // Computed getters
  isPrimary: () => {
    return get().currentMode === "primary";
  },

  isObserver: () => {
    return get().currentMode === "observer";
  },

  isQueued: () => {
    return get().currentMode === "queued";
  },

  isPending: () => {
    return get().currentMode === "pending";
  },

  canRequestPrimary: () => {
    const state = get();
    return state.currentMode === "observer" &&
           !state.isRequestingPrimary &&
           state.sessions.some(s => s.mode === "primary");
  },

  getPrimarySession: () => {
    return get().sessions.find(s => s.mode === "primary");
  },

  getQueuePosition: () => {
    const state = get();
    if (state.currentMode !== "queued") return -1;

    const queuedSessions = state.sessions
      .filter(s => s.mode === "queued")
      .sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());

    return queuedSessions.findIndex(s => s.id === state.currentSessionId) + 1;
  }
    }),
    {
      name: 'session',
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({
        currentSessionId: state.currentSessionId,
      }),
    }
  )
);

// Shared session store - separate with localStorage (shared across tabs)
// Used for user preferences that should be consistent across all tabs
export interface SharedSessionState {
  nickname: string | null;
  setNickname: (nickname: string | null) => void;
  clearNickname: () => void;
}

export const useSharedSessionStore = create<SharedSessionState>()(
  persist(
    (set) => ({
      nickname: null,
      setNickname: (nickname: string | null) => set({ nickname }),
      clearNickname: () => set({ nickname: null }),
    }),
    {
      name: 'sharedSession',
      storage: createJSONStorage(() => localStorage),
    }
  )
);