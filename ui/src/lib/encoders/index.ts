/**
 * Internal encoder implementations for camera passthrough.
 *
 * These are internal implementation details. Use CameraEncoder from
 * cameraEncoder.ts for the public API.
 *
 * @internal
 */

export { H264Encoder, isH264Supported, isVideoEncoderSupported } from "./H264Encoder";
export { MjpegEncoder, isMjpegSupported, hasRequestVideoFrameCallback } from "./MjpegEncoder";
export type {
  VideoCodec,
  EncoderConfig,
  EncodedFrame,
  InternalEncoderEvents,
  InternalEncoder,
} from "./types";
