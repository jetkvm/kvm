import { m } from "@localizations/messages.js";

// Consent is scoped to this browser tab and device, never shared across devices.
export const microphoneStorageKey = (deviceId?: string) => `jetkvm.microphone.${deviceId ?? ""}`;

export function preferMicrophoneCodec(transceiver: RTCRtpTransceiver) {
  const codecs = RTCRtpSender.getCapabilities("audio")?.codecs.filter(
    (codec) => codec.mimeType.toLowerCase() === "audio/pcmu",
  );
  if (!codecs?.length) throw new Error(m.audio_microphone_codec_unsupported());
  transceiver.setCodecPreferences(codecs);
}
