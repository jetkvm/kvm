// Package vnc implements a high-performance VNC (RFB protocol) server.
//
// This package provides a VNC server implementation optimized for embedded
// systems with hardware video encoding support. It implements the Remote
// Framebuffer (RFB) protocol as specified in RFC 6143.
//
// # Features
//
//   - RFB Protocol 3.8 with VeNCrypt TLS extension
//   - Tight encoding with hardware-accelerated JPEG compression
//   - VNC Authentication (DES challenge-response)
//   - Keyboard and mouse input forwarding via HID
//   - Clipboard text typing support
//
// # Performance Considerations
//
// The implementation is optimized for resource-constrained embedded devices:
//
//   - Pre-allocated buffers minimize heap allocations
//   - Atomic operations for lock-free hot paths
//   - Struct layout optimized for cache efficiency on 32-bit ARM
//   - Buffer pools for temporary allocations
//   - Zero-copy frame sending where possible
//
// # Usage
//
// Create a server with dependencies:
//
//	deps := vnc.Dependencies{
//		Config:  myConfig,
//		Encoder: myEncoder,
//		HID:     myHIDController,
//		TLS:     myTLSProvider,
//		Logger:  myLogger,
//	}
//	server := vnc.NewServer(deps)
//	if err := server.Start(5900); err != nil {
//		log.Fatal(err)
//	}
//
// # File Organization
//
// The package is organized by concern:
//
//   - protocol.go: RFB protocol types and constants
//   - server.go: Server lifecycle and connection management
//   - connection.go: Connection struct and core methods
//   - handshake.go: Protocol version negotiation
//   - auth.go: Authentication (VeNCrypt, VNCAuth, TLS)
//   - messages.go: Message loop and handlers
//   - frame.go: Frame encoding and sending
//   - input.go: Keyboard and mouse handling
//   - keyboard.go: Keysym to HID mappings
//   - clipboard.go: Clipboard text typing
package vnc
