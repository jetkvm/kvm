import { useCallback, useEffect, useRef, useState } from 'react';
import useWebSocket, { ReadyState } from 'react-use-websocket';

import { devError, devWarn } from '../utils/debug';
import { NETWORK_CONFIG } from '../config/constants';

import { JsonRpcResponse, useJsonRpc } from './useJsonRpc';
import { useRTCStore } from './stores';

// Audio event types matching the backend
export type AudioEventType = 
  | 'audio-mute-changed'
  | 'microphone-state-changed'
  | 'audio-device-changed';

// Audio event data interfaces
export interface AudioMuteData {
  muted: boolean;
}

export interface MicrophoneStateData {
  running: boolean;
  session_active: boolean;
}

export interface AudioDeviceChangedData {
  enabled: boolean;
  reason: string;
}

// Audio event structure
export interface AudioEvent {
  type: AudioEventType;
  data: AudioMuteData | MicrophoneStateData | AudioDeviceChangedData;
}

// Hook return type
export interface UseAudioEventsReturn {
  // Connection state
  connectionState: ReadyState;
  isConnected: boolean;
  
  // Audio state
  audioMuted: boolean | null;
  
  // Microphone state
  microphoneState: MicrophoneStateData | null;
  
  // Device change events
  onAudioDeviceChanged?: (data: AudioDeviceChangedData) => void;
  
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

export function useAudioEvents(onAudioDeviceChanged?: (data: AudioDeviceChangedData) => void): UseAudioEventsReturn {
  // State for audio data
  const [audioMuted, setAudioMuted] = useState<boolean | null>(null);
  const [microphoneState, setMicrophoneState] = useState<MicrophoneStateData | null>(null);
  
  // Get RTC store and JSON RPC functionality
  const { rpcDataChannel } = useRTCStore();
  const { send } = useJsonRpc();
  
  // Fetch initial audio status using RPC for cloud compatibility
  const fetchInitialAudioStatus = useCallback(async () => {
    // Early return if RPC data channel is not open
    if (rpcDataChannel?.readyState !== "open") {
      devWarn('RPC connection not available for initial audio status, skipping');
      return;
    }

    try {
      await new Promise<void>((resolve) => {
        send("audioStatus", {}, (resp: JsonRpcResponse) => {
          if ("error" in resp) {
            devError('RPC audioStatus failed:', resp.error);
          } else if ("result" in resp) {
            const data = resp.result as { muted: boolean };
            setAudioMuted(data.muted);
          }
          resolve(); // Continue regardless of result
        });
      });
    } catch (error) {
      devError('Failed to fetch initial audio status via RPC:', error);
    }
  }, [rpcDataChannel?.readyState, send]);
  
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
    reconnectInterval: NETWORK_CONFIG.WEBSOCKET_RECONNECT_INTERVAL,
    share: true, // Share the WebSocket connection across multiple hooks
    onOpen: () => {
      // WebSocket connected
      // Reset global state on new connection
      globalSubscriptionState.isSubscribed = false;
      globalSubscriptionState.connectionId = Math.random().toString(36);
    },
    onClose: () => {
      // WebSocket disconnected
      // Reset global state on disconnect
      globalSubscriptionState.isSubscribed = false;
      globalSubscriptionState.subscriberCount = 0;
      globalSubscriptionState.connectionId = null;
    },
    onError: (event) => {
      devError('[AudioEvents] WebSocket error:', event);
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
          // Subscribed to audio events
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
        // Sent unsubscribe message to backend
      }
    }
    
    // Component unsubscribed from audio events
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
              // Audio mute changed
              break;
            }
              
            case 'microphone-state-changed': {
              const micStateData = audioEvent.data as MicrophoneStateData;
              setMicrophoneState(micStateData);
              // Microphone state changed
              break;
            }
              
            case 'audio-device-changed': {
              const deviceChangedData = audioEvent.data as AudioDeviceChangedData;
              // Audio device changed
              if (onAudioDeviceChanged) {
                onAudioDeviceChanged(deviceChangedData);
              }
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
          devWarn('[AudioEvents] Failed to parse WebSocket message:', error);
        }
      }
    }
  }, [lastMessage, onAudioDeviceChanged]);

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

  // Fetch initial audio status on component mount - but only when RPC is ready
  useEffect(() => {
    // Only fetch when RPC data channel is open and ready
    if (rpcDataChannel?.readyState === "open") {
      fetchInitialAudioStatus();
    }
  }, [fetchInitialAudioStatus, rpcDataChannel?.readyState]);

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
    
    // Microphone state
    microphoneState,
    
    // Device change events
    onAudioDeviceChanged,
    
    // Manual subscription control
    subscribe,
    unsubscribe,
  };
}