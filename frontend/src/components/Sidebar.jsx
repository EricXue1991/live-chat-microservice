import { useState, useEffect } from 'react';
import { getRooms } from '../utils/api';

/**
 * Sidebar — room list + user info + connection status.
 * Rooms are now fetched from PostgreSQL via GET /api/rooms.
 */
export default function Sidebar({ currentRoom, onRoomChange, username, onLogout, connected }) {
  const [rooms, setRooms] = useState([]);
  const [customRoom, setCustomRoom] = useState('');

  // Fetch rooms from PostgreSQL on mount
  useEffect(() => {
    getRooms().then(data => {
      if (data.rooms) setRooms(data.rooms);
    }).catch(() => {
      // Fallback if API not ready
      setRooms([
        { id: 'room-general', name: 'General', description: 'General discussion' },
        { id: 'room-hot', name: 'Hot Room', description: 'Viral room (Experiment 2)' },
        { id: 'room-tech', name: 'Tech Talk', description: 'Technology chat' },
        { id: 'room-random', name: 'Random', description: 'Off-topic fun' },
      ]);
    });
  }, []);

  const icons = { 'room-general': '💬', 'room-hot': '🔥', 'room-tech': '💻', 'room-random': '🎲', 'room-music': '🎵' };

  const handleJoinCustom = (e) => {
    e.preventDefault();
    if (customRoom.trim()) { onRoomChange(customRoom.trim()); setCustomRoom(''); }
  };

  return (
    <div className="h-full flex flex-col" style={{ background: 'var(--bg-secondary)', borderRight: '1px solid var(--border)' }}>
      {/* Header */}
      <div className="px-4 py-4" style={{ borderBottom: '1px solid var(--border)' }}>
        <div className="flex items-center justify-between">
          <h1 className="text-lg font-bold tracking-tight">💬 LiveChat</h1>
          <div className="flex items-center gap-1.5">
            <div className={`w-2 h-2 rounded-full ${connected ? 'status-pulse' : ''}`}
                 style={{ background: connected ? 'var(--success)' : 'var(--danger)' }} />
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
              {connected ? 'Connected' : 'Offline'}
            </span>
          </div>
        </div>
      </div>

      {/* Room list */}
      <div className="flex-1 overflow-y-auto py-3 px-2">
        <div className="px-2 mb-2">
          <span className="text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Rooms</span>
        </div>
        {rooms.map(room => (
          <button key={room.id} onClick={() => onRoomChange(room.id)}
                  className="w-full text-left px-3 py-2.5 rounded-lg mb-0.5 transition-colors flex items-center gap-3"
                  style={{ background: currentRoom === room.id ? 'var(--bg-hover)' : 'transparent',
                           color: currentRoom === room.id ? 'var(--text-primary)' : 'var(--text-secondary)' }}>
            <span className="text-lg">{icons[room.id] || '📢'}</span>
            <div className="min-w-0">
              <div className="text-sm font-medium truncate">{room.name}</div>
              <div className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>{room.description}</div>
            </div>
          </button>
        ))}

        {/* Custom room input */}
        <div className="mt-4 px-2">
          <span className="text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Join Custom Room</span>
          <form onSubmit={handleJoinCustom} className="mt-2 flex gap-1.5">
            <input type="text" value={customRoom} onChange={e => setCustomRoom(e.target.value)}
                   placeholder="room-id" className="flex-1 px-3 py-2 rounded-lg text-xs outline-none font-mono"
                   style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)', color: 'var(--text-primary)' }} />
            <button type="submit" className="px-3 py-2 rounded-lg text-xs font-medium text-white" style={{ background: 'var(--accent)' }}>Join</button>
          </form>
        </div>
      </div>

      {/* User info */}
      <div className="px-4 py-3 flex items-center justify-between" style={{ borderTop: '1px solid var(--border)' }}>
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold text-white"
               style={{ background: 'var(--accent)' }}>
            {username?.[0]?.toUpperCase() || '?'}
          </div>
          <span className="text-sm font-medium truncate max-w-[100px]">{username}</span>
        </div>
        <button onClick={onLogout} className="text-xs px-3 py-1.5 rounded-lg"
                style={{ color: 'var(--text-secondary)', background: 'var(--bg-tertiary)' }}>Logout</button>
      </div>
    </div>
  );
}
