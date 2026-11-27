const CONNECT_TIMEOUT_MS = 4000;
const DECODE_TIMEOUT_MS = 4000;
const POLL_MS = 100;

export interface SelfTestResult {
  connected: boolean;
  h264: boolean | null;
}

const isH264 = (codec: { mimeType: string }) => codec.mimeType.toLowerCase() === "video/h264";

async function until(check: () => boolean | Promise<boolean>, timeoutMs: number) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await check()) return true;
    await new Promise(resolve => setTimeout(resolve, POLL_MS));
  }
  return false;
}

async function videoFrames(
  connection: RTCPeerConnection,
  type: "inbound-rtp" | "outbound-rtp",
  key: "framesDecoded" | "framesEncoded",
) {
  let frames = 0;
  (await connection.getStats()).forEach(report => {
    if (report.type === type && report.kind === "video") frames = report[key] ?? 0;
  });
  return frames;
}

export async function runSelfTest(): Promise<SelfTestResult> {
  let sender: RTCPeerConnection;
  let receiver: RTCPeerConnection;
  try {
    sender = new RTCPeerConnection();
    receiver = new RTCPeerConnection();
  } catch {
    return { connected: false, h264: null };
  }

  const canvas = document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 240;
  const context = canvas.getContext("2d");
  let hue = 0;
  const draw = setInterval(() => {
    if (!context) return;
    hue = (hue + 30) % 360;
    context.fillStyle = `hsl(${hue} 80% 50%)`;
    context.fillRect(0, 0, canvas.width, canvas.height);
  }, POLL_MS);
  const stream = canvas.captureStream(10);

  const video = document.createElement("video");
  video.muted = true;
  video.playsInline = true;

  try {
    sender.onicecandidate = ({ candidate }) => {
      if (candidate) void receiver.addIceCandidate(candidate).catch(() => undefined);
    };
    receiver.onicecandidate = ({ candidate }) => {
      if (candidate) void sender.addIceCandidate(candidate).catch(() => undefined);
    };
    receiver.ontrack = ({ track }) => {
      video.srcObject = new MediaStream([track]);
      void video.play().catch(() => undefined);
    };

    sender.addTransceiver(stream.getVideoTracks()[0], { direction: "sendonly" });
    sender.createDataChannel("self-test");

    const offer = await sender.createOffer();
    await sender.setLocalDescription(offer);
    await receiver.setRemoteDescription(offer);

    const decodable = RTCRtpReceiver.getCapabilities?.("video")?.codecs.filter(isH264) ?? [];
    let h264Negotiated = /H264/i.test(offer.sdp ?? "") && decodable.length > 0;
    if (h264Negotiated) {
      try {
        receiver.getTransceivers()[0]?.setCodecPreferences(decodable);
      } catch {
        h264Negotiated = false;
      }
    }

    const answer = await receiver.createAnswer();
    await receiver.setLocalDescription(answer);
    await sender.setRemoteDescription(answer);

    const connected = await until(
      () => receiver.connectionState === "connected",
      CONNECT_TIMEOUT_MS,
    );
    if (!connected) return { connected: false, h264: null };
    if (!h264Negotiated) return { connected: true, h264: null };

    const decoded = await until(
      async () => (await videoFrames(receiver, "inbound-rtp", "framesDecoded")) > 0,
      DECODE_TIMEOUT_MS,
    );
    if (decoded) return { connected: true, h264: true };
    const encoded = await videoFrames(sender, "outbound-rtp", "framesEncoded");
    return { connected: true, h264: encoded > 0 ? false : null };
  } catch {
    return { connected: false, h264: null };
  } finally {
    clearInterval(draw);
    stream.getTracks().forEach(track => track.stop());
    video.srcObject = null;
    sender.close();
    receiver.close();
  }
}
