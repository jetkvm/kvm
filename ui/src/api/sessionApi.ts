import { SessionInfo } from "@/stores/sessionStore";

interface JsonRpcResponse {
  result?: unknown;
  error?: { message: string };
}

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: JsonRpcResponse) => void) => void;

export const sessionApi = {
  getSessions: async (sendFn: RpcSendFunction): Promise<SessionInfo[]> => {
    return new Promise((resolve, reject) => {
      sendFn("getSessions", {}, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve((response.result as SessionInfo[]) || []);
        }
      });
    });
  },

  getSessionInfo: async (sendFn: RpcSendFunction, sessionId: string): Promise<SessionInfo> => {
    return new Promise((resolve, reject) => {
      sendFn("getSessionInfo", { sessionId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve(response.result as SessionInfo);
        }
      });
    });
  },

  requestPrimary: async (sendFn: RpcSendFunction, sessionId: string): Promise<{ status: string; mode?: string; message?: string }> => {
    return new Promise((resolve, reject) => {
      sendFn("requestPrimary", { sessionId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve(response.result as { status: string; mode?: string; message?: string });
        }
      });
    });
  },

  releasePrimary: async (sendFn: RpcSendFunction, sessionId: string): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("releasePrimary", { sessionId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  transferPrimary: async (
    sendFn: RpcSendFunction,
    fromId: string,
    toId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("transferPrimary", { fromId, toId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  updateNickname: async (
    sendFn: RpcSendFunction,
    sessionId: string,
    nickname: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("updateSessionNickname", { sessionId, nickname }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  approveNewSession: async (
    sendFn: RpcSendFunction,
    sessionId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("approveNewSession", { sessionId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  },

  denyNewSession: async (
    sendFn: RpcSendFunction,
    sessionId: string
  ): Promise<void> => {
    return new Promise((resolve, reject) => {
      sendFn("denyNewSession", { sessionId }, (response: JsonRpcResponse) => {
        if (response.error) {
          reject(new Error(response.error.message));
        } else {
          resolve();
        }
      });
    });
  }
};