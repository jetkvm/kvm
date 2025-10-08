// Nickname generation using backend API for consistency

type RpcSendFunction = (method: string, params: Record<string, unknown>, callback: (response: { result?: unknown; error?: { message: string } }) => void) => void;

// Main function that uses backend generation
export async function generateNickname(sendFn?: RpcSendFunction): Promise<string> {
  // Require backend function - no fallback
  if (!sendFn) {
    throw new Error('Backend connection required for nickname generation');
  }

  return new Promise((resolve, reject) => {
    try {
      const result = sendFn('generateNickname', { userAgent: navigator.userAgent }, (response: { result?: unknown; error?: { message: string } }) => {
        const result = response.result as { nickname?: string } | undefined;
        if (response && !response.error && result?.nickname) {
          resolve(result.nickname);
        } else {
          reject(new Error('Failed to generate nickname from backend'));
        }
      });

      // If sendFn returns undefined (RPC channel not ready), reject immediately
      if (result === undefined) {
        reject(new Error('RPC connection not ready yet'));
      }
    } catch (error) {
      reject(error);
    }
  });
}

// Synchronous version removed - backend generation is always async
export function generateNicknameSync(): string {
  throw new Error('Synchronous nickname generation not supported - use backend generateNickname()');
}