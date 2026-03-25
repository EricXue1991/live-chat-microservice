/**
 * API utility — centralizes all backend calls.
 * Automatically attaches JWT token to requests.
 */

const API_BASE = import.meta.env.VITE_API_URL || '';
const WS_BASE = import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}`;

export const getToken = () => localStorage.getItem('lc_token');
export const setToken = (t) => localStorage.setItem('lc_token', t);
export const getUsername = () => localStorage.getItem('lc_username');
export const setUsername = (u) => localStorage.setItem('lc_username', u);
export const clearAuth = () => { localStorage.removeItem('lc_token'); localStorage.removeItem('lc_username'); };

async function authFetch(url, opts = {}) {
  const token = getToken();
  const headers = { 'Content-Type': 'application/json', ...opts.headers };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${API_BASE}${url}`, { ...opts, headers });
  if (res.status === 401) { clearAuth(); window.location.reload(); }
  return res;
}

export async function register(username, password) {
  const res = await fetch(`${API_BASE}/api/register`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  return res.json();
}

export async function login(username, password) {
  const res = await fetch(`${API_BASE}/api/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json();
  if (res.ok && data.token) { setToken(data.token); setUsername(data.username); }
  return { ok: res.ok, data };
}

export async function getRooms() {
  const res = await authFetch('/api/rooms');
  return res.json();
}

export async function sendMessage(roomId, content, attachmentUrl = '') {
  const res = await authFetch('/api/messages', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, content, attachment_url: attachmentUrl }),
  });
  return res.json();
}

export async function getMessages(roomId, since = 0) {
  const res = await authFetch(`/api/messages?roomId=${roomId}&since=${since}`);
  return res.json();
}

export async function submitReaction(roomId, reactionType) {
  const res = await authFetch('/api/reactions', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, reaction_type: reactionType }),
  });
  return res.json();
}

export async function getReactions(roomId) {
  const res = await authFetch(`/api/reactions?roomId=${roomId}`);
  return res.json();
}

export async function getAnalytics(roomId) {
  const url = roomId ? `/api/analytics?roomId=${roomId}` : '/api/analytics';
  const res = await authFetch(url);
  return res.json();
}

export async function uploadFile(file) {
  const token = getToken();
  const form = new FormData();
  form.append('file', file);
  const res = await fetch(`${API_BASE}/api/upload`, {
    method: 'POST', headers: { 'Authorization': `Bearer ${token}` }, body: form,
  });
  return res.json();
}

export function connectWebSocket(roomId, { onMessage, onOpen, onClose, onError }) {
  const token = getToken();
  const ws = new WebSocket(`${WS_BASE}/ws/rooms/${roomId}?token=${token}`);
  ws.onopen = () => { console.log(`[WS] connected room=${roomId}`); onOpen?.(); };
  ws.onmessage = (e) => { try { onMessage?.(JSON.parse(e.data)); } catch {} };
  ws.onclose = (e) => { onClose?.(e); };
  ws.onerror = (e) => { onError?.(e); };
  return ws;
}

export function wsSendMessage(ws, content, attachmentUrl = '') {
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'chat', payload: { content, attachment_url: attachmentUrl } }));
  }
}
