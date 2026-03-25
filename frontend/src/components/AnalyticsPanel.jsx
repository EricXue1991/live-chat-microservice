import { useState, useEffect } from 'react';
import { getAnalytics } from '../utils/api';

/**
 * AnalyticsPanel — displays real-time room analytics from the Kafka consumer.
 * Shows message count, reaction count, unique users, top reactions.
 */
export default function AnalyticsPanel({ roomId }) {
  const [stats, setStats] = useState(null);

  useEffect(() => {
    if (!roomId) return;
    const fetch = async () => {
      try {
        const data = await getAnalytics(roomId);
        setStats(data);
      } catch {}
    };
    fetch();
    const iv = setInterval(fetch, 5000);
    return () => clearInterval(iv);
  }, [roomId]);

  if (!stats || (!stats.message_count && !stats.reaction_count)) {
    return null; // Don't render if no analytics data
  }

  return (
    <div className="px-4 py-2 flex items-center gap-4 text-xs"
         style={{ borderTop: '1px solid var(--border)', color: 'var(--text-muted)' }}>
      <span className="font-mono">📊 Analytics</span>
      <span>💬 {stats.message_count || 0} msgs</span>
      <span>🎉 {stats.reaction_count || 0} reactions</span>
      <span>👥 {stats.user_count || 0} users</span>
      {stats.top_reactions && Object.keys(stats.top_reactions).length > 0 && (
        <span>Top: {Object.entries(stats.top_reactions).sort((a, b) => b[1] - a[1]).slice(0, 3).map(([k, v]) => `${k}(${v})`).join(' ')}</span>
      )}
    </div>
  );
}
