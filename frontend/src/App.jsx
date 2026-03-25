import { useState } from 'react';
import LoginPage from './components/LoginPage';
import Sidebar from './components/Sidebar';
import ChatRoom from './components/ChatRoom';
import { getToken, getUsername, clearAuth } from './utils/api';

export default function App() {
  const [loggedIn, setLoggedIn] = useState(!!getToken());
  const [username, setUsername] = useState(getUsername() || '');
  const [currentRoom, setCurrentRoom] = useState('room-general');

  if (!loggedIn) {
    return <LoginPage onLogin={(name) => { setUsername(name); setLoggedIn(true); }} />;
  }

  return (
    <div className="h-screen flex overflow-hidden">
      <div className="w-64 shrink-0 hidden md:block">
        <Sidebar currentRoom={currentRoom} onRoomChange={setCurrentRoom}
                 username={username} onLogout={() => { clearAuth(); setLoggedIn(false); }}
                 connected={true} />
      </div>
      <div className="md:hidden fixed top-0 left-0 right-0 z-10 px-4 py-2 flex items-center justify-between"
           style={{ background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border)' }}>
        <span className="text-sm font-bold">💬 {currentRoom}</span>
        <button onClick={() => { clearAuth(); setLoggedIn(false); }} className="text-xs" style={{ color: 'var(--text-secondary)' }}>Logout</button>
      </div>
      <div className="flex-1 md:pt-0 pt-10">
        <ChatRoom key={currentRoom} roomId={currentRoom} username={username} />
      </div>
    </div>
  );
}
