package kvm

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4/pkg/media"
)

var (
	videoStreamSubscribersMu sync.RWMutex
	videoStreamSubscribers   = map[chan []byte]struct{}{}

	streamViewerSessionsMu sync.RWMutex
	streamViewerSessions   = map[*Session]struct{}{}
)

func getActiveStreamClients() int {
	videoStreamSubscribersMu.RLock()
	defer videoStreamSubscribersMu.RUnlock()

	return len(videoStreamSubscribers)
}

func hasActiveVideoConsumers() bool {
	return getActiveSessions() > 0 || getActiveStreamClients() > 0
}

func onFirstVideoConsumerConnected() {
	_ = nativeInstance.VideoStart()
	stopVideoSleepModeTicker()
}

func onLastVideoConsumerDisconnected() {
	_ = nativeInstance.VideoStop()
	startVideoSleepModeTicker()
}

func subscribeVideoStream() chan []byte {
	ch := make(chan []byte, 4)

	videoStreamSubscribersMu.Lock()
	videoStreamSubscribers[ch] = struct{}{}
	activeStreamClients := len(videoStreamSubscribers)
	videoStreamSubscribersMu.Unlock()

	if activeStreamClients == 1 && getActiveSessions() == 0 {
		onFirstVideoConsumerConnected()
	}

	return ch
}

func unsubscribeVideoStream(ch chan []byte) {
	videoStreamSubscribersMu.Lock()
	delete(videoStreamSubscribers, ch)
	activeStreamClients := len(videoStreamSubscribers)
	videoStreamSubscribersMu.Unlock()

	if activeStreamClients == 0 && getActiveSessions() == 0 {
		onLastVideoConsumerDisconnected()
	}
}

func publishVideoFrame(frame []byte) {
	videoStreamSubscribersMu.RLock()
	if len(videoStreamSubscribers) == 0 {
		videoStreamSubscribersMu.RUnlock()
		return
	}

	frameCopy := append([]byte(nil), frame...)
	for subscriber := range videoStreamSubscribers {
		select {
		case subscriber <- frameCopy:
		default:
			// Drop frames for slow clients to keep live latency low.
		}
	}
	videoStreamSubscribersMu.RUnlock()
}

func registerStreamViewerSession(session *Session) {
	streamViewerSessionsMu.Lock()
	streamViewerSessions[session] = struct{}{}
	streamViewerSessionsMu.Unlock()
}

func getActiveStreamViewerSessions() int {
	streamViewerSessionsMu.RLock()
	defer streamViewerSessionsMu.RUnlock()

	return len(streamViewerSessions)
}

func unregisterStreamViewerSession(session *Session) {
	streamViewerSessionsMu.Lock()
	delete(streamViewerSessions, session)
	streamViewerSessionsMu.Unlock()
}

func publishVideoToStreamViewerSessions(frame []byte, duration time.Duration) {
	streamViewerSessionsMu.RLock()
	if len(streamViewerSessions) == 0 {
		streamViewerSessionsMu.RUnlock()
		return
	}

	sessions := make([]*Session, 0, len(streamViewerSessions))
	for session := range streamViewerSessions {
		sessions = append(sessions, session)
	}
	streamViewerSessionsMu.RUnlock()

	for _, session := range sessions {
		if session == nil || session.VideoTrack == nil {
			continue
		}

		if err := session.VideoTrack.WriteSample(media.Sample{Data: frame, Duration: duration}); err != nil {
			nativeLogger.Warn().Err(err).Msg("error writing sample to stream viewer session")
		}
	}
}

const streamViewerHTML = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Video Stream</title>
	<style>
		:root {
			--bg: #0f1419;
			--panel: #1a222c;
			--text: #f3f5f7;
			--muted: #9aa6b2;
			--accent: #2bb1ff;
			--danger: #ff6b6b;
		}
		html, body {
			margin: 0;
			padding: 0;
			width: 100%;
			height: 100%;
			background: radial-gradient(circle at 20% 10%, #233140 0%, var(--bg) 42%);
			color: var(--text);
			font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
		}
		.layout {
			display: flex;
			flex-direction: column;
			gap: 12px;
			height: 100%;
			padding: 12px;
			box-sizing: border-box;
		}
		.toolbar {
			display: flex;
			gap: 10px;
			align-items: center;
			background: color-mix(in srgb, var(--panel) 82%, transparent);
			border: 1px solid #2f3b48;
			border-radius: 12px;
			padding: 10px;
		}
		.title {
			font-size: 14px;
			letter-spacing: 0.04em;
			text-transform: uppercase;
			color: var(--muted);
			margin-right: auto;
		}
		button {
			border: 1px solid #3c4b5b;
			background: #243140;
			color: var(--text);
			padding: 8px 12px;
			border-radius: 8px;
			cursor: pointer;
			font-weight: 600;
		}
		button:hover {
			border-color: var(--accent);
		}
		#status {
			font-size: 13px;
			color: var(--muted);
		}
		.stage {
			flex: 1;
			min-height: 0;
			border-radius: 12px;
			overflow: hidden;
			border: 1px solid #2f3b48;
			background: #000;
			position: relative;
		}
		video {
			width: 100%;
			height: 100%;
			object-fit: contain;
			background: #000;
		}
		.error {
			color: var(--danger);
		}
	</style>
</head>
<body>
	<main class="layout">
		<section class="toolbar">
			<div class="title">Video Stream</div>
			<div id="status">Initializing...</div>
			<button id="reconnect" type="button">Reconnect</button>
			<button id="fullscreen" type="button">Fullscreen</button>
		</section>
		<section class="stage" id="stage">
			<video id="video" autoplay playsinline muted></video>
		</section>
	</main>
	<script>
		const statusEl = document.getElementById("status");
		const videoEl = document.getElementById("video");
		const stageEl = document.getElementById("stage");
		const reconnectEl = document.getElementById("reconnect");
		const fullscreenEl = document.getElementById("fullscreen");

		let pc = null;
		let reconnectTimer = null;
		let reconnectAttempts = 0;
		let intentionalClose = false;

		const reconnectConfig = {
			initialDelayMs: 1000,
			maxDelayMs: 10000,
			fetchTimeoutMs: 10000,
		};

		function setStatus(message, isError = false) {
			statusEl.textContent = message;
			statusEl.className = isError ? "error" : "";
		}

		function waitForIceGatheringComplete(peerConnection) {
			if (peerConnection.iceGatheringState === "complete") {
				return Promise.resolve();
			}
			return new Promise((resolve) => {
				const onStateChange = () => {
					if (peerConnection.iceGatheringState === "complete") {
						peerConnection.removeEventListener("icegatheringstatechange", onStateChange);
						resolve();
					}
				};
				peerConnection.addEventListener("icegatheringstatechange", onStateChange);
			});
		}

		function clearReconnectTimer() {
			if (reconnectTimer) {
				clearTimeout(reconnectTimer);
				reconnectTimer = null;
			}
		}

		function closePeerConnection() {
			if (pc) {
				try {
					pc.ontrack = null;
					pc.oniceconnectionstatechange = null;
					pc.onconnectionstatechange = null;
					pc.close();
				} catch (_) {}
				pc = null;
			}
		}

		function scheduleReconnect(reason) {
			if (intentionalClose) {
				return;
			}
			if (reconnectTimer) {
				return;
			}

			const delay = Math.min(
				reconnectConfig.initialDelayMs * Math.pow(2, reconnectAttempts),
				reconnectConfig.maxDelayMs,
			);

			reconnectAttempts += 1;
			setStatus("Reconnecting in " + Math.ceil(delay / 1000) + "s (" + reason + ")", true);

			reconnectTimer = setTimeout(() => {
				reconnectTimer = null;
				connect().catch((error) => {
					scheduleReconnect(error.message || "connect error");
				});
			}, delay);
		}

		async function connect() {
			clearReconnectTimer();
			closePeerConnection();

			pc = new RTCPeerConnection();
			pc.addTransceiver("video", { direction: "recvonly" });

			pc.ontrack = (event) => {
				console.log("ontrack event:", event);
				setStatus("Track received");
				reconnectAttempts = 0;
				if (event.streams && event.streams[0]) {
					videoEl.srcObject = event.streams[0];
					console.log("Video source set");
				}
			};

			pc.oniceconnectionstatechange = () => {
				console.log("ICE state:", pc.iceConnectionState);
				setStatus("ICE: " + pc.iceConnectionState);

				if (["failed", "disconnected", "closed"].includes(pc.iceConnectionState)) {
					scheduleReconnect("ice " + pc.iceConnectionState);
				}
			};

			pc.onconnectionstatechange = () => {
				console.log("Connection state:", pc.connectionState);
				setStatus("Connection: " + pc.connectionState);

				if (["failed", "disconnected", "closed"].includes(pc.connectionState)) {
					scheduleReconnect("peer " + pc.connectionState);
				}
			};

			setStatus("Creating offer...");
			const offer = await pc.createOffer();
			await pc.setLocalDescription(offer);
			await waitForIceGatheringComplete(pc);

			const encodedOffer = btoa(JSON.stringify(pc.localDescription));
			setStatus("Requesting session...");
			const controller = new AbortController();
			const timeout = setTimeout(() => controller.abort(), reconnectConfig.fetchTimeoutMs);
			const response = await fetch("/webrtc/stream-session", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				credentials: "same-origin",
				body: JSON.stringify({ sd: encodedOffer }),
				signal: controller.signal,
			});
			clearTimeout(timeout);

			if (!response.ok) {
				const errorText = await response.text();
				throw new Error("Session error " + response.status + ": " + errorText);
			}

			setStatus("Processing answer...");
			const payload = await response.json();
			const answerJson = atob(payload.sd);
			const answer = JSON.parse(answerJson);
			await pc.setRemoteDescription(answer);

			setStatus("Waiting for connection...");
			reconnectAttempts = 0;
		}

		reconnectEl.addEventListener("click", async () => {
			try {
				reconnectAttempts = 0;
				setStatus("Reconnecting...");
				await connect();
			} catch (error) {
				scheduleReconnect(error.message || "Reconnect failed");
			}
		});

		fullscreenEl.addEventListener("click", async () => {
			try {
				if (document.fullscreenElement) {
					await document.exitFullscreen();
				} else {
					if (stageEl.requestFullscreen) {
						await stageEl.requestFullscreen();
					} else if (stageEl.webkitRequestFullscreen) {
						stageEl.webkitRequestFullscreen();
					}
				}
			} catch (error) {
				setStatus(error.message || "Fullscreen failed", true);
			}
		});

		window.addEventListener("offline", () => {
			setStatus("Network offline", true);
		});

		window.addEventListener("online", () => {
			scheduleReconnect("network online");
		});

		window.addEventListener("beforeunload", () => {
			intentionalClose = true;
			clearReconnectTimer();
			closePeerConnection();
		});

		connect().catch((error) => {
			scheduleReconnect(error.message || "Connection failed");
		});
	</script>
</body>
</html>`

func handleStreamPage(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(streamViewerHTML))
}

func handleWebRTCStreamSession(c *gin.Context) {
	connectionID := uuid.New().String()
	scopedLogger := webrtcLogger.With().Str("component", "stream-session").Str("connectionID", connectionID).Logger()

	var req WebRTCSessionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := newSession(SessionConfig{
		MDNSMode: config.NetworkConfig.MDNSMode.String,
		Logger:   &scopedLogger,
		OnClosed: func(session *Session) {
			unregisterStreamViewerSession(session)
			scopedLogger.Info().Int("activeStreamViewerSessions", getActiveStreamViewerSessions()).Msg("stream viewer session closed")
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	sd, err := session.ExchangeOffer(req.Sd)
	if err != nil {
		_ = session.peerConnection.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	registerStreamViewerSession(session)
	scopedLogger.Info().Int("activeStreamViewerSessions", getActiveStreamViewerSessions()).Msg("stream viewer session created")
	c.JSON(http.StatusOK, gin.H{"sd": sd})
}

func handleRawVideoStream(c *gin.Context) {
	subscriber := subscribeVideoStream()
	defer unsubscribeVideoStream(subscriber)

	c.Header("Content-Type", "video/h264")
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	for {
		select {
		case frame := <-subscriber:
			if _, err := c.Writer.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}
