import { useEffect, useRef, useState, useCallback } from 'react';
import { connectWebSocket, wsSendMessage } from '../utils/api';

/**
 * useWebSocket — manages WebSocket lifecycle with auto-reconnect.
 * Exponential backoff: 1s, 2s, 4s, ... up to 30s max.
 */
export function useWebSocket(roomId, onMessage) {
  const wsRef = useRef(null);
  const [connected, setConnected] = useState(false);
  const [online, setOnline] = useState(0);
  const reconnectTimer = useRef(null);
  const attempts = useRef(0);

  const connect = useCallback(() => {
    if (!roomId) return;
    if (wsRef.current) wsRef.current.close();

    wsRef.current = connectWebSocket(roomId, {
      onOpen: () => { setConnected(true); attempts.current = 0; },
      onMessage: (data) => {
        if (data.type === 'system' && data.payload?.online) setOnline(data.payload.online);
        onMessage?.(data);
      },
      onClose: () => {
        setConnected(false);
        const delay = Math.min(1000 * Math.pow(2, attempts.current), 30000);
        attempts.current++;
        reconnectTimer.current = setTimeout(connect, delay);
      },
      onError: () => {},
    });
  }, [roomId, onMessage]);

  useEffect(() => {
    connect();
    return () => {
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      if (wsRef.current) wsRef.current.close();
    };
  }, [connect]);

  const send = useCallback((content, attachmentUrl) => {
    wsSendMessage(wsRef.current, content, attachmentUrl);
  }, []);

  return { send, connected, online };
}
