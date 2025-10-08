import { SessionInfo } from "@/stores/sessionStore";

export const sessionApi = {
  getSessions: async (sendFn: Function): Promise<SessionInfo[]> => {
    return new Promise((resolve, reject) => {
      sendFn("getSessions", {}, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve(response.result || []);
        }
      });
    });
  },

  getSessionInfo: async (sendFn: Function, sessionId: string): Promise<SessionInfo> => {
    return new Promise((resolve, reject) => {
      sendFn("getSessionInfo", { sessionId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve(response.result);
        }
      });
    });
  },

  requestPrimary: async (sendFn: Function, sessionId: string): Promise<{ status: string; mode?: string; message?: string }> => {
    return new Promise((resolve, reject) => {
      sendFn("requestPrimary", { sessionId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve(response.result);
        }
      });
    });
  },

  releasePrimary: async (sendFn: Function, sessionId: string): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("releasePrimary", { sessionId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  transferPrimary: async (
    sendFn: Function,
    fromId: string,
    toId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("transferPrimary", { fromId, toId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  updateNickname: async (
    sendFn: Function,
    sessionId: string,
    nickname: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("updateSessionNickname", { sessionId, nickname }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  approveNewSession: async (
    sendFn: Function,
    sessionId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("approveNewSession", { sessionId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  denyNewSession: async (
    sendFn: Function,
    sessionId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("denyNewSession", { sessionId }, (response: any) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  }
};