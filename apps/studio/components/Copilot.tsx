'use client';
import { useState } from 'react';
import type { NodeDescriptor } from '@crosscraft/engine';
import type { GraphOp, Workflow } from '@crosscraft/schema';

interface Props {
  workflow: () => Workflow;
  descriptors: NodeDescriptor[];
  onApply: (ops: GraphOp[]) => void;
  onClose: () => void;
}

interface Msg {
  role: 'user' | 'assistant';
  text: string;
}

export function Copilot({ workflow, onApply, onClose }: Props) {
  const [msgs, setMsgs] = useState<Msg[]>([
    { role: 'assistant', text: 'Describe a workflow and I will build it on the canvas. e.g. "When a webhook arrives, summarize the text with AI, then branch if urgent."' },
  ]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);

  const send = async () => {
    if (!input.trim() || busy) return;
    const message = input.trim();
    setInput('');
    setMsgs((m) => [...m, { role: 'user', text: message }]);
    setBusy(true);
    try {
      const res = await fetch('/api/copilot', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ message, workflow: workflow() }),
      });
      const data = (await res.json()) as { ops?: GraphOp[]; message?: string; error?: string };
      if (data.error) {
        setMsgs((m) => [...m, { role: 'assistant', text: '⚠ ' + data.error }]);
      } else {
        if (data.ops?.length) onApply(data.ops);
        setMsgs((m) => [
          ...m,
          { role: 'assistant', text: data.message || `Applied ${data.ops?.length ?? 0} change(s) to the canvas.` },
        ]);
      }
    } catch (e) {
      setMsgs((m) => [...m, { role: 'assistant', text: '⚠ ' + (e as Error).message }]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ width: 340, borderLeft: '1px solid var(--border)', display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: 12, borderBottom: '1px solid var(--border)' }}>
        <span style={{ fontWeight: 600, fontSize: 14 }}>✨ Copilot</span>
        <button className="btn" style={{ padding: '4px 8px' }} onClick={onClose}>
          ✕
        </button>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
        {msgs.map((m, i) => (
          <div
            key={i}
            style={{
              alignSelf: m.role === 'user' ? 'flex-end' : 'flex-start',
              maxWidth: '90%',
              background: m.role === 'user' ? 'var(--accent)' : 'var(--panel-2)',
              color: m.role === 'user' ? '#fff' : 'var(--text)',
              border: m.role === 'user' ? 'none' : '1px solid var(--border)',
              borderRadius: 10,
              padding: '8px 11px',
              fontSize: 13,
              whiteSpace: 'pre-wrap',
            }}
          >
            {m.text}
          </div>
        ))}
        {busy && <div style={{ color: 'var(--muted)', fontSize: 13 }}>thinking…</div>}
      </div>
      <div style={{ padding: 12, borderTop: '1px solid var(--border)', display: 'flex', gap: 8 }}>
        <input
          className="input"
          placeholder="Describe a workflow…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && send()}
        />
        <button className="btn btn-accent" onClick={send} disabled={busy}>
          Send
        </button>
      </div>
    </div>
  );
}
