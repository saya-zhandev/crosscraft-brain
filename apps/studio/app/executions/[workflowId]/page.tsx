'use client';
import { use, useEffect, useState } from 'react';
import Link from 'next/link';
import { api, type ExecutionRow } from '@/lib/client';
import type { StepRecord } from '@crosscraft/schema';

const STATUS_COLOR: Record<string, string> = {
  running: 'var(--warn)',
  waiting: 'var(--wait)',
  success: 'var(--ok)',
  error: 'var(--err)',
};

export default function Executions({ params }: { params: Promise<{ workflowId: string }> }) {
  const { workflowId } = use(params);
  const [rows, setRows] = useState<ExecutionRow[]>([]);
  const [openId, setOpenId] = useState<string | null>(null);
  const [detail, setDetail] = useState<{ status: string; steps: StepRecord[] } | null>(null);

  useEffect(() => {
    api.listExecutions(workflowId).then(setRows);
  }, [workflowId]);

  const open = async (id: string) => {
    setOpenId(id);
    setDetail(null);
    const d = await fetch(`/api/executions/${id}`).then((r) => r.json());
    setDetail(d);
  };

  return (
    <div style={{ maxWidth: 1000, margin: '0 auto', padding: 24 }}>
      <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 16 }}>
        <Link href={`/editor/${workflowId}`} className="btn">
          ← Editor
        </Link>
        <h1 style={{ fontSize: 18, margin: 0 }}>Runs</h1>
        <span style={{ color: 'var(--muted)', fontSize: 13 }}>Transparent monitoring — every node's input & output.</span>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: openId ? '320px 1fr' : '1fr', gap: 16 }}>
        <div style={{ display: 'grid', gap: 6, alignContent: 'start' }}>
          {rows.length === 0 && <p style={{ color: 'var(--muted)' }}>No runs yet.</p>}
          {rows.map((r) => (
            <button
              key={r.id}
              onClick={() => open(r.id)}
              style={{
                textAlign: 'left',
                padding: '10px 12px',
                background: openId === r.id ? 'var(--panel-2)' : 'var(--panel)',
                border: '1px solid var(--border)',
                borderRadius: 8,
                cursor: 'pointer',
                color: 'var(--text)',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                <span style={{ fontSize: 12, fontFamily: 'monospace' }}>{r.id.slice(0, 10)}</span>
                <span style={{ fontSize: 12, color: STATUS_COLOR[r.status] }}>● {r.status}</span>
              </div>
              <div style={{ fontSize: 11, color: 'var(--muted)' }}>{new Date(r.startedAt).toLocaleString()}</div>
            </button>
          ))}
        </div>

        {openId && (
          <div>
            {!detail ? (
              <p style={{ color: 'var(--muted)' }}>Loading…</p>
            ) : (
              <div style={{ display: 'grid', gap: 10 }}>
                <div style={{ fontSize: 13, color: 'var(--muted)' }}>
                  status: <span style={{ color: STATUS_COLOR[detail.status] }}>{detail.status}</span>
                </div>
                {detail.steps.map((s) => (
                  <div key={s.id} style={{ border: '1px solid var(--border)', borderRadius: 8, padding: 10, background: 'var(--panel)' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                      <b style={{ fontSize: 13 }}>{s.nodeId}</b>
                      <span style={{ fontSize: 12, color: STATUS_COLOR[s.status] }}>{s.status}</span>
                    </div>
                    {s.error && <pre style={pre('var(--err)')}>{s.error}</pre>}
                    <div style={{ fontSize: 11, color: 'var(--muted)' }}>output</div>
                    <pre style={pre()}>{JSON.stringify(s.output, null, 2)}</pre>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

const pre = (color?: string): React.CSSProperties => ({
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  padding: 8,
  fontSize: 11,
  maxHeight: 180,
  overflow: 'auto',
  whiteSpace: 'pre-wrap',
  color: color ?? 'var(--text)',
});
