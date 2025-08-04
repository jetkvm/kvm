import { useCallback, useEffect, useRef, useState } from 'react';
import useWebSocket, { ReadyState } from 'react-use-websocket';

// Audio event types matching the backend
export type AudioEventType = 
  | 'audio-mute-changed'
  | 'audio-metrics-update'
  | 'microphone-state-changed'
  | 'microphone-metrics-update';

// Audio event data interfaces
export interface AudioMuteData {
  muted: boolean;
}

export interface AudioMetricsData {
  frames_received: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
}

export interface MicrophoneStateData {
  running: boolean;
  session_active: boolean;
}

export interface MicrophoneMetricsData {
  frames_sent: number;
  frames_dropped: number;
  bytes_processed: number;
  last_frame_time: string;
  connection_drops: number;
  average_latency: string;
}

// Audio event structure
export interface AudioEvent {
  type: AudioEventType;
  data: AudioMuteData | AudioMetricsData | MicrophoneStateData | MicrophoneMetricsData;
}

// Hook return type
export interface UseAudioEventsReturn {
  // Connection state
  connectionState: ReadyState;
  isConnected: boolean;
  
  // Audio state
  audioMuted: boolean | null;
  audioMetrics: AudioMetricsData | null;
  
  // Microphone state
  microphoneState: MicrophoneStateData | null;
  microphoneMetrics: MicrophoneMetricsData | null;
  
  // Manual subscription control
  subscribe: () => void;
  unsubscribe: () => void;
}

export function useAudioEvents(): UseAudioEventsReturn {
  // State for audio data
  const [audioMuted, setAudioMuted] = useState<boolean | null>(null);
  const [audioMetrics, setAudioMetrics] = useState<AudioMetricsData | null>(null);
  const [microphoneState, setMicrophoneState] = useState<MicrophoneStateData | null>(null);
  const [microphoneMetrics, setMicrophoneMetrics] = useState<MicrophoneMetricsData | null>(null);
  
  // Subscription state
  const [isSubscribed, setIsSubscribed] = useState(false);
  const subscriptionSent = useRef(false);

  // Get WebSocket URL
  const getWebSocketUrl = () => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    return `${protocol}//${host}/webrtc/signaling/client`;
  };

  // WebSocket connection
  const {
    sendMessage,
    lastMessage,
    readyState,
  } = useWebSocket(getWebSocketUrl(), {
    shouldReconnect: () => true,
    reconnectAttempts: 10,
    reconnectInterval: 3000,
    onOpen: () => {
      console.log('[AudioEvents] WebSocket connected');
      subscriptionSent.current = false;
    },
    onClose: () => {
      console.log('[AudioEvents] WebSocket disconnected');
      subscriptionSent.current = false;
      setIsSubscribed(false);
    },
    onError: (event) => {
      console.error('[AudioEvents] WebSocket error:', event);
    },
  });

  // Subscribe to audio events
  const subscribe = useCallback(() => {
    if (readyState === ReadyState.OPEN && !subscriptionSent.current) {
      const subscribeMessage = {
        type: 'subscribe-audio-events',
        data: {}
      };
      
      sendMessage(JSON.stringify(subscribeMessage));
      subscriptionSent.current = true;
      setIsSubscribed(true);
      console.log('[AudioEvents] Subscribed to audio events');
    }
  }, [readyState, sendMessage]);

  // Handle incoming messages
  useEffect(() => {
    if (lastMessage !== null) {
      try {
        const message = JSON.parse(lastMessage.data);
        
        // Handle audio events
        if (message.type && message.data) {
          const audioEvent = message as AudioEvent;
          
          switch (audioEvent.type) {
            case 'audio-mute-changed': {
              const muteData = audioEvent.data as AudioMuteData;
              setAudioMuted(muteData.muted);
              console.log('[AudioEvents] Audio mute changed:', muteData.muted);
              break;
            }
              
            case 'audio-metrics-update': {
              const audioMetricsData = audioEvent.data as AudioMetricsData;
              setAudioMetrics(audioMetricsData);
              break;
            }
              
            case 'microphone-state-changed': {
              const micStateData = audioEvent.data as MicrophoneStateData;
              setMicrophoneState(micStateData);
              console.log('[AudioEvents] Microphone state changed:', micStateData);
              break;
            }
              
            case 'microphone-metrics-update': {
              const micMetricsData = audioEvent.data as MicrophoneMetricsData;
              setMicrophoneMetrics(micMetricsData);
              break;
            }
              
            default:
              // Ignore other message types (WebRTC signaling, etc.)
              break;
          }
        }
      } catch (error) {
        // Ignore parsing errors for non-JSON messages (like "pong")
        if (lastMessage.data !== 'pong') {
          console.warn('[AudioEvents] Failed to parse WebSocket message:', error);
        }
      }
    }
  }, [lastMessage]);

  // Auto-subscribe when connected
  useEffect(() => {
    if (readyState === ReadyState.OPEN && !subscriptionSent.current) {
      subscribe();
    }
  }, [readyState, subscribe]);

  // Unsubscribe from audio events (connection will be cleaned up automatically)
  const unsubscribe = useCallback(() => {
    setIsSubscribed(false);
    subscriptionSent.current = false;
    console.log('[AudioEvents] Unsubscribed from audio events');
  }, []);

  return {
    // Connection state
    connectionState: readyState,
    isConnected: readyState === ReadyState.OPEN && isSubscribed,
    
    // Audio state
    audioMuted,
    audioMetrics,
    
    // Microphone state
    microphoneState,
    microphoneMetrics,
    
    // Manual subscription control
    subscribe,
    unsubscribe,
  };
}