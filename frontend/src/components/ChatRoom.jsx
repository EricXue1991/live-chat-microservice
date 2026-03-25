import { useState, useCallback } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';
import MessageList from './MessageList';
import MessageInput from './MessageInput';
import ReactionBar from './ReactionBar';
import AnalyticsPanel from './AnalyticsPanel';

export default function ChatRoom({ roomId, username }) {
  const [messages, setMessages] = useState([]);

  const handleWSMessage = useCallback((data) => {
    if (data.type === 'chat' && data.payload) {
      setMessages(prev => {
        if (prev.some(m => m.message_id === data.payload.message_id)) return prev;
        return [...prev, data.payload];
      });
    }
  }, []);

  const { send, connected, online } = useWebSocket(roomId, handleWSMessage);

  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-primary)' }}>
      {/* Room header */}
      <div className="px-4 py-3 flex items-center justify-between shrink-0"
           style={{ borderBottom: '1px solid var(--border)' }}>
        <div>
          <h2 className="text-sm font-semibold">{roomId}</h2>
          <div className="flex items-center gap-2 mt-0.5">
            <div className={`w-1.5 h-1.5 rounded-full ${connected ? 'status-pulse' : ''}`}
                 style={{ background: connected ? 'var(--success)' : 'var(--danger)' }} />
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
              {connected ? `${online} online` : 'Connecting...'}
            </span>
          </div>
        </div>
        <div className="text-xs px-3 py-1 rounded-full font-mono"
             style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
          WS {connected ? '✓' : '✗'}
        </div>
      </div>

      <MessageList messages={messages} currentUser={username} />
      <AnalyticsPanel roomId={roomId} />
      <ReactionBar roomId={roomId} />
      <MessageInput onSend={(c, a) => send(c, a)} disabled={!connected} />
    </div>
  );
}
