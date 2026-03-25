import { useEffect, useRef } from 'react';

export default function MessageList({ messages, currentUser }) {
  const bottomRef = useRef(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <div className="text-4xl mb-3">🏠</div>
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>No messages yet. Start the conversation!</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto px-4 py-3 space-y-1">
      {messages.map((msg, idx) => {
        const isMe = msg.username === currentUser;
        const showAvatar = idx === 0 || messages[idx - 1].username !== msg.username;
        const time = new Date(msg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

        return (
          <div key={msg.message_id || idx} className="message-enter">
            {showAvatar && (
              <div className={`flex items-center gap-2 mt-3 mb-1 ${isMe ? 'justify-end' : ''}`}>
                {!isMe && (
                  <div className="w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold text-white"
                       style={{ background: stringToColor(msg.username) }}>
                    {msg.username[0]?.toUpperCase()}
                  </div>
                )}
                <span className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
                  {isMe ? 'You' : msg.username}
                </span>
                <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{time}</span>
              </div>
            )}
            <div className={`flex ${isMe ? 'justify-end' : 'justify-start'}`}>
              <div className="max-w-[70%] px-3.5 py-2 rounded-xl text-sm leading-relaxed"
                   style={{
                     background: isMe ? 'var(--accent)' : 'var(--bg-tertiary)',
                     color: isMe ? '#fff' : 'var(--text-primary)',
                     borderBottomRightRadius: isMe ? '4px' : undefined,
                     borderBottomLeftRadius: !isMe ? '4px' : undefined,
                   }}>
                {msg.content}
                {msg.attachment_url && (
                  <div className="mt-2">
                    {/\.(jpg|jpeg|png|gif|webp)(\?.*)?$/i.test(msg.attachment_url)
                      ? <img src={msg.attachment_url} alt="attachment" className="max-w-full rounded-lg max-h-48 object-cover" />
                      : <a href={msg.attachment_url} target="_blank" rel="noopener noreferrer" className="text-xs underline opacity-80">📎 View attachment</a>
                    }
                  </div>
                )}
              </div>
            </div>
          </div>
        );
      })}
      <div ref={bottomRef} />
    </div>
  );
}

function stringToColor(str) {
  let hash = 0;
  for (let i = 0; i < str.length; i++) hash = str.charCodeAt(i) + ((hash << 5) - hash);
  return `hsl(${Math.abs(hash) % 360}, 60%, 45%)`;
}
