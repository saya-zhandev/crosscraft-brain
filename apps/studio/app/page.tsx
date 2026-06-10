'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api } from '@/lib/client';
import type { Workflow } from '@crosscraft/schema';

export default function Home() {
  const [wfs, setWfs] = useState<Pick<Workflow, 'id' | 'name' | 'active'>[]>([]);
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(true);

  const refresh = () => api.listWorkflows().then(setWfs).finally(() => setLoading(false));
  useEffect(() => {
    refresh();
  }, []);

  const create = async () => {
    const wf = await api.createWorkflow(name || 'Untitled workflow');
    location.href = `/editor/${wf.id}`;
  };

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: '48px 20px' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 12 }}>
        <h1 style={{ margin: 0, fontSize: 24 }}>crosscraft</h1>
        <span style={{ color: 'var(--muted)', fontSize: 13 }}>workflow studio</span>
      </div>
      <p style={{ color: 'var(--muted)', fontSize: 14 }}>
        Visual editor · integrations · AI · transparent monitoring.
      </p>

      <div style={{ display: 'flex', gap: 8, margin: '24px 0' }}>
        <input
          className="input"
          placeholder="New workflow name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && create()}
        />
        <button className="btn btn-accent" onClick={create}>
          Create
        </button>
      </div>

      <h2 style={{ fontSize: 13, textTransform: 'uppercase', color: 'var(--muted)' }}>Workflows</h2>
      {loading ? (
        <p style={{ color: 'var(--muted)' }}>Loading…</p>
      ) : wfs.length === 0 ? (
        <p style={{ color: 'var(--muted)' }}>No workflows yet.</p>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {wfs.map((w) => (
            <Link
              key={w.id}
              href={`/editor/${w.id}`}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                padding: '14px 16px',
                background: 'var(--panel)',
                border: '1px solid var(--border)',
                borderRadius: 10,
                textDecoration: 'none',
                color: 'var(--text)',
              }}
            >
              <span>{w.name}</span>
              <span style={{ fontSize: 12, color: w.active ? 'var(--ok)' : 'var(--muted)' }}>
                {w.active ? '● active' : 'inactive'}
              </span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
