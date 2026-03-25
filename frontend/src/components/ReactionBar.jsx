import { useState, useEffect, useCallback } from 'react';
import { submitReaction, getReactions } from '../utils/api';

const REACTIONS = [
  { type: 'like', emoji: '👍' }, { type: 'love', emoji: '❤️' },
  { type: 'laugh', emoji: '😂' }, { type: 'fire', emoji: '🔥' },
  { type: 'surprise', emoji: '😮' }, { type: 'sad', emoji: '😢' },
];

export default function ReactionBar({ roomId }) {
  const [counts, setCounts] = useState({});
  const [animating, setAnimating] = useState('');

  const fetchCounts = useCallback(async () => {
    if (!roomId) return;
    try {
      const data = await getReactions(roomId);
      if (data.reactions) {
        const map = {};
        data.reactions.forEach(r => { map[r.reaction_type] = r.count; });
        setCounts(map);
      }
    } catch {}
  }, [roomId]);

  useEffect(() => {
    fetchCounts();
    const iv = setInterval(fetchCounts, 3000);
    return () => clearInterval(iv);
  }, [fetchCounts]);

  const handleReaction = async (type) => {
    setCounts(prev => ({ ...prev, [type]: (prev[type] || 0) + 1 }));
    setAnimating(type);
    setTimeout(() => setAnimating(''), 300);
    try { await submitReaction(roomId, type); } catch {
      setCounts(prev => ({ ...prev, [type]: Math.max(0, (prev[type] || 0) - 1) }));
    }
  };

  const fmt = (n) => {
    if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k';
    return String(n);
  };

  return (
    <div className="px-4 py-2 flex items-center gap-1.5 flex-wrap" style={{ borderTop: '1px solid var(--border)' }}>
      <span className="text-xs mr-1" style={{ color: 'var(--text-muted)' }}>Reactions:</span>
      {REACTIONS.map(({ type, emoji }) => (
        <button key={type} onClick={() => handleReaction(type)}
                className={`flex items-center gap-1 px-2.5 py-1 rounded-full text-xs transition-all hover:scale-105 ${animating === type ? 'reaction-pop' : ''}`}
                style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)', color: 'var(--text-primary)' }}>
          <span className="text-base">{emoji}</span>
          {counts[type] > 0 && <span className="font-mono font-semibold" style={{ color: 'var(--accent)' }}>{fmt(counts[type])}</span>}
        </button>
      ))}
    </div>
  );
}
