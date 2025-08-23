import { useCallback, useEffect, useRef, useState } from 'react';
import useWebSocket, { ReadyState } from 'react-use-websocket';

// Audio event types matching the backend
export type AudioEventType = 
  | 'audio-mute-changed'
  | 'audio-metrics-update'
  | 'microphone-state-changed'
  | 'microphone-metrics-update'
  | 'audio-process-metrics'
  | 'microphone-process-metrics';

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

export interface ProcessMetricsData {
  pid: number;
  cpu_percent: number;
  memory_rss: number;
  memory_vms: number;
  memory_percent: number;
  running: boolean;
  process_name: string;
}

// Audio event structure
export interface AudioEvent {
  type: AudioEventType;
  data: AudioMuteData | AudioMetricsData | MicrophoneStateData | MicrophoneMetricsData | ProcessMetricsData;
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
  
  // Process metrics
  audioProcessMetrics: ProcessMetricsData | null;
  microphoneProcessMetrics: ProcessMetricsData | null;
  
  // Manual subscription control
  subscribe: () => void;
  unsubscribe: () => void;
}

// Global subscription management to prevent multiple subscriptions per WebSocket connection
const globalSubscriptionState = {
  isSubscribed: false,
  subscriberCount: 0,
  connectionId: null as string | null
};

export function useAudioEvents(): UseAudioEventsReturn {
  // State for audio data
  const [audioMuted, setAudioMuted] = useState<boolean | null>(null);
  const [audioMetrics, setAudioMetrics] = useState<AudioMetricsData | null>(null);
  const [microphoneState, setMicrophoneState] = useState<MicrophoneStateData | null>(null);
  const [microphoneMetrics, setMicrophoneMetricsData] = useState<MicrophoneMetricsData | null>(null);
  const [audioProcessMetrics, setAudioProcessMetrics] = useState<ProcessMetricsData | null>(null);
  const [microphoneProcessMetrics, setMicrophoneProcessMetrics] = useState<ProcessMetricsData | null>(null);
  
  // Local subscription state
  const [isLocallySubscribed, setIsLocallySubscribed] = useState(false);
  const subscriptionTimeoutRef = useRef<number | null>(null);

  // Get WebSocket URL
  const getWebSocketUrl = () => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    return `${protocol}//${host}/webrtc/signaling/client`;
  };

  // Shared WebSocket connection using the `share` option for better resource management
  const {
    sendMessage,
    lastMessage,
    readyState,
  } = useWebSocket(getWebSocketUrl(), {
    shouldReconnect: () => true,
    reconnectAttempts: 10,
    reconnectInterval: 3000,
    share: true, // Share the WebSocket connection across multiple hooks
    onOpen: () => {
      console.log('[AudioEvents] WebSocket connected');
      // Reset global state on new connection
      globalSubscriptionState.isSubscribed = false;
      globalSubscriptionState.connectionId = Math.random().toString(36);
    },
    onClose: () => {
      console.log('[AudioEvents] WebSocket disconnected');
      // Reset global state on disconnect
      globalSubscriptionState.isSubscribed = false;
      globalSubscriptionState.subscriberCount = 0;
      globalSubscriptionState.connectionId = null;
    },
    onError: (event) => {
      console.error('[AudioEvents] WebSocket error:', event);
    },
  });

  // Subscribe to audio events
  const subscribe = useCallback(() => {
    if (readyState === ReadyState.OPEN && !globalSubscriptionState.isSubscribed) {
      // Clear any pending subscription timeout
      if (subscriptionTimeoutRef.current) {
        clearTimeout(subscriptionTimeoutRef.current);
        subscriptionTimeoutRef.current = null;
      }

      // Add a small delay to prevent rapid subscription attempts
      subscriptionTimeoutRef.current = setTimeout(() => {
        if (readyState === ReadyState.OPEN && !globalSubscriptionState.isSubscribed) {
          const subscribeMessage = {
            type: 'subscribe-audio-events',
            data: {}
          };
          
          sendMessage(JSON.stringify(subscribeMessage));
          globalSubscriptionState.isSubscribed = true;
          console.log('[AudioEvents] Subscribed to audio events');
        }
      }, 100); // 100ms delay to debounce subscription attempts
    }
    
    // Track local subscription regardless of global state
    if (!isLocallySubscribed) {
      globalSubscriptionState.subscriberCount++;
      setIsLocallySubscribed(true);
    }
  }, [readyState, sendMessage, isLocallySubscribed]);

  // Unsubscribe from audio events
  const unsubscribe = useCallback(() => {
    // Clear any pending subscription timeout
    if (subscriptionTimeoutRef.current) {
      clearTimeout(subscriptionTimeoutRef.current);
      subscriptionTimeoutRef.current = null;
    }

    if (isLocallySubscribed) {
      globalSubscriptionState.subscriberCount--;
      setIsLocallySubscribed(false);
      
      // Only send unsubscribe message if this is the last subscriber and connection is still open
      if (globalSubscriptionState.subscriberCount <= 0 && 
          readyState === ReadyState.OPEN && 
          globalSubscriptionState.isSubscribed) {
        
        const unsubscribeMessage = {
          type: 'unsubscribe-audio-events',
          data: {}
        };
        
        sendMessage(JSON.stringify(unsubscribeMessage));
        globalSubscriptionState.isSubscribed = false;
        globalSubscriptionState.subscriberCount = 0;
        console.log('[AudioEvents] Sent unsubscribe message to backend');
      }
    }
    
    console.log('[AudioEvents] Component unsubscribed from audio events');
  }, [readyState, isLocallySubscribed, sendMessage]);

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
              setMicrophoneMetricsData(micMetricsData);
              break;
            }
              
            case 'audio-process-metrics': {
              const audioProcessData = audioEvent.data as ProcessMetricsData;
              setAudioProcessMetrics(audioProcessData);
              break;
            }
              
            case 'microphone-process-metrics': {
              const micProcessData = audioEvent.data as ProcessMetricsData;
              setMicrophoneProcessMetrics(micProcessData);
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
    if (readyState === ReadyState.OPEN) {
      subscribe();
    }
    
    // Cleanup subscription on component unmount or connection change
    return () => {
      if (subscriptionTimeoutRef.current) {
        clearTimeout(subscriptionTimeoutRef.current);
        subscriptionTimeoutRef.current = null;
      }
      unsubscribe();
    };
  }, [readyState, subscribe, unsubscribe]);

  // Reset local subscription state on disconnect
  useEffect(() => {
    if (readyState === ReadyState.CLOSED || readyState === ReadyState.CLOSING) {
      setIsLocallySubscribed(false);
      if (subscriptionTimeoutRef.current) {
        clearTimeout(subscriptionTimeoutRef.current);
        subscriptionTimeoutRef.current = null;
      }
    }
  }, [readyState]);

  // Cleanup on component unmount
  useEffect(() => {
    return () => {
      unsubscribe();
    };
  }, [unsubscribe]);

  return {
    // Connection state
    connectionState: readyState,
    isConnected: readyState === ReadyState.OPEN && globalSubscriptionState.isSubscribed,
    
    // Audio state
    audioMuted,
    audioMetrics,
    
    // Microphone state
    microphoneState,
    microphoneMetrics: microphoneMetrics,
    
    // Process metrics
    audioProcessMetrics,
    microphoneProcessMetrics,
    
    // Manual subscription control
    subscribe,
    unsubscribe,
  };
}