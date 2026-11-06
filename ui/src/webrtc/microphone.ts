import { m } from "@localizations/messages.js";

// Consent is scoped to this browser tab and device, never shared across devices.
export const microphoneStorageKey = (deviceId?: string) => `jetkvm.microphone.${deviceId ?? ""}`;

export function preferMicrophoneCodec(transceiver: RTCRtpTransceiver) {
  // Opus first for wideband voice; PCMU stays as the fallback every device accepts.
  const supported = RTCRtpSender.getCapabilities("audio")?.codecs ?? [];
  const codecs = ["audio/opus", "audio/pcmu"]
    .flatMap((mime) => supported.filter((codec) => codec.mimeType.toLowerCase() === mime));
  if (!codecs?.length) throw new Error(m.audio_microphone_codec_unsupported());
  transceiver.setCodecPreferences(codecs);
}
