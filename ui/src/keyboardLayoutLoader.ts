import type { JsonRpcResponse } from "@/hooks/useJsonRpc";

type RpcChannel = Pick<
  RTCDataChannel,
  "readyState" | "addEventListener" | "removeEventListener"
>;
type RpcSend = (
  method: string,
  params: unknown,
  callback: (resp: JsonRpcResponse) => void,
) => unknown;

// One fetch per rpc channel, so a reconnect or another device fetches again.
// A fetch without a usable answer drops its entry and the next caller retries.
const loads = new WeakMap<RpcChannel, Promise<boolean>>();

// Resolves true once the device's keyboard layout is stored, false once it
// cannot be: the channel closed, the request failed or the reply timed out.
// Nothing is asked before the channel is open.
export function loadKeyboardLayout(
  channel: RpcChannel | null,
  send: RpcSend,
  setKeyboardLayout: (layout: string) => void,
  timeoutMs = 3000,
): Promise<boolean> {
  if (
    !channel ||
    channel.readyState === "closing" ||
    channel.readyState === "closed"
  )
    return Promise.resolve(false);
  const pending = loads.get(channel);
  if (pending) return pending;

  let resolve!: (loaded: boolean) => void;
  const load = new Promise<boolean>((r) => (resolve = r));
  loads.set(channel, load);

  let timer: ReturnType<typeof setTimeout> | undefined;
  const finish = (loaded: boolean) => {
    clearTimeout(timer);
    channel.removeEventListener("open", request);
    channel.removeEventListener("close", giveUp);
    if (!loaded && loads.get(channel) === load) loads.delete(channel);
    resolve(loaded);
  };
  const giveUp = () => finish(false);
  const request = () => {
    timer = setTimeout(giveUp, timeoutMs);
    void send("getKeyboardLayout", {}, (resp) => {
      if ("error" in resp) return giveUp();
      setKeyboardLayout(resp.result as string);
      finish(true);
    });
  };
  channel.addEventListener("close", giveUp);
  if (channel.readyState === "open") request();
  else channel.addEventListener("open", request);
  return load;
}
