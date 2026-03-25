import { useState, useRef } from 'react';
import { uploadFile } from '../utils/api';

export default function MessageInput({ onSend, disabled }) {
  const [text, setText] = useState('');
  const [uploading, setUploading] = useState(false);
  const [attachmentUrl, setAttachmentUrl] = useState('');
  const [attachmentName, setAttachmentName] = useState('');
  const fileRef = useRef(null);

  const handleSend = () => {
    const content = text.trim();
    if (!content && !attachmentUrl) return;
    onSend(content || '📎 Attachment', attachmentUrl);
    setText(''); setAttachmentUrl(''); setAttachmentName('');
  };

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend(); }
  };

  const handleFile = async (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (file.size > 10 * 1024 * 1024) { alert('File too large (max 10MB)'); return; }
    setUploading(true);
    try {
      const r = await uploadFile(file);
      if (r.url) { setAttachmentUrl(r.url); setAttachmentName(file.name); }
    } catch { alert('Upload failed'); }
    setUploading(false);
    e.target.value = '';
  };

  return (
    <div className="px-4 py-3" style={{ borderTop: '1px solid var(--border)' }}>
      {attachmentName && (
        <div className="flex items-center gap-2 mb-2 px-3 py-2 rounded-lg text-xs"
             style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>
          <span>📎 {attachmentName}</span>
          <button onClick={() => { setAttachmentUrl(''); setAttachmentName(''); }}
                  className="ml-auto" style={{ color: 'var(--danger)' }}>✕</button>
        </div>
      )}
      <div className="flex items-end gap-2">
        <button onClick={() => fileRef.current?.click()} disabled={uploading || disabled}
                className="p-2.5 rounded-lg shrink-0"
                style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
                title="Upload file">{uploading ? '⏳' : '📎'}</button>
        <input ref={fileRef} type="file" className="hidden" onChange={handleFile} accept="image/*,.pdf,.doc,.txt" />

        <textarea value={text} onChange={e => setText(e.target.value)} onKeyDown={handleKeyDown}
                  disabled={disabled} rows={1}
                  placeholder={disabled ? 'Connecting...' : 'Type a message... (Enter to send)'}
                  className="flex-1 px-4 py-2.5 rounded-xl text-sm outline-none resize-none"
                  style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border)',
                           color: 'var(--text-primary)', maxHeight: '120px' }} />

        <button onClick={handleSend} disabled={disabled || (!text.trim() && !attachmentUrl)}
                className="p-2.5 rounded-lg shrink-0 text-white"
                style={{ background: disabled || (!text.trim() && !attachmentUrl) ? 'var(--text-muted)' : 'var(--accent)' }}>➤</button>
      </div>
    </div>
  );
}
