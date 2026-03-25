import { useState } from 'react';
import { login, register } from '../utils/api';

export default function LoginPage({ onLogin }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [isRegister, setIsRegister] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      if (isRegister) {
        const r = await register(username, password);
        if (r.error) { setError(r.error); setLoading(false); return; }
      }
      const { ok, data } = await login(username, password);
      if (ok) onLogin(data.username);
      else setError(data.error || 'Login failed');
    } catch { setError('Network error — is the backend running?'); }
    setLoading(false);
  };

  return (
    <div className="min-h-screen flex items-center justify-center" style={{ background: 'var(--bg-primary)' }}>
      <div className="w-full max-w-md px-8">
        <div className="text-center mb-10">
          <div className="text-5xl mb-3">💬</div>
          <h1 className="text-3xl font-bold tracking-tight" style={{ color: 'var(--text-primary)' }}>LiveChat</h1>
          <p className="mt-2 text-sm" style={{ color: 'var(--text-secondary)' }}>Distributed real-time chat + reactions</p>
        </div>
        <form onSubmit={handleSubmit} className="rounded-xl p-8 space-y-5"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}>
          <div>
            <label className="block text-xs font-medium mb-1.5 uppercase tracking-wider"
                   style={{ color: 'var(--text-secondary)' }}>Username</label>
            <input type="text" value={username} onChange={e => setUsername(e.target.value)}
                   required minLength={2} maxLength={20}
                   className="w-full px-4 py-3 rounded-lg text-sm outline-none"
                   style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)', color: 'var(--text-primary)' }}
                   placeholder="Enter username" />
          </div>
          <div>
            <label className="block text-xs font-medium mb-1.5 uppercase tracking-wider"
                   style={{ color: 'var(--text-secondary)' }}>Password</label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)}
                   required minLength={6}
                   className="w-full px-4 py-3 rounded-lg text-sm outline-none"
                   style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)', color: 'var(--text-primary)' }}
                   placeholder="Min 6 characters" />
          </div>
          {error && <div className="px-4 py-2.5 rounded-lg text-sm" style={{ background: 'rgba(255,82,82,0.1)', color: 'var(--danger)' }}>{error}</div>}
          <button type="submit" disabled={loading}
                  className="w-full py-3 rounded-lg font-semibold text-sm text-white"
                  style={{ background: loading ? 'var(--text-muted)' : 'var(--accent)', cursor: loading ? 'not-allowed' : 'pointer' }}>
            {loading ? '...' : isRegister ? 'Create Account & Login' : 'Login'}
          </button>
          <div className="text-center">
            <button type="button" onClick={() => { setIsRegister(!isRegister); setError(''); }}
                    className="text-sm hover:underline" style={{ color: 'var(--accent)' }}>
              {isRegister ? 'Already have an account? Login' : "Don't have an account? Register"}
            </button>
          </div>
        </form>
        <p className="text-center mt-6 text-xs" style={{ color: 'var(--text-muted)' }}>CS6650 Distributed Systems Project</p>
      </div>
    </div>
  );
}
