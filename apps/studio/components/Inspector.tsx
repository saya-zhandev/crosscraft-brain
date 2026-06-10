'use client';
import type { NodeDescriptor } from '@crosscraft/engine';
import type { ParamSchema, StepRecord } from '@crosscraft/schema';

interface Props {
  descriptor: NodeDescriptor;
  name: string;
  params: Record<string, unknown>;
  step?: StepRecord; // last run's data for this node (transparent monitoring)
  onRename: (name: string) => void;
  onChange: (params: Record<string, unknown>) => void;
  onDelete: () => void;
}

function visible(p: ParamSchema, params: Record<string, unknown>): boolean {
  if (!p.showWhen) return true;
  return p.showWhen.equals.includes(params[p.showWhen.param]);
}

export function Inspector({ descriptor, name, params, step, onRename, onChange, onDelete }: Props) {
  const set = (k: string, v: unknown) => onChange({ ...params, [k]: v });

  const asText = (v: unknown) => (v == null ? '' : typeof v === 'object' ? JSON.stringify(v, null, 2) : String(v));

  return (
    <div style={{ width: 340, borderLeft: '1px solid var(--border)', overflowY: 'auto', padding: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span style={{ fontSize: 12, color: 'var(--muted)' }}>{descriptor.label}</span>
        <button className="btn" onClick={onDelete} style={{ padding: '4px 8px', fontSize: 12 }}>
          Delete
        </button>
      </div>

      <label className="fld">Node name</label>
      <input className="input" value={name} onChange={(e) => onRename(e.target.value)} />

      {descriptor.params.filter((p) => visible(p, params)).map((p) => {
        const val = params[p.name] ?? p.default ?? '';
        return (
          <div key={p.name}>
            <label className="fld">
              {p.label}
              {p.required ? ' *' : ''}
            </label>
            {p.type === 'boolean' ? (
              <input
                type="checkbox"
                checked={Boolean(val)}
                onChange={(e) => set(p.name, e.target.checked)}
              />
            ) : p.type === 'select' ? (
              <select className="input" value={String(val)} onChange={(e) => set(p.name, e.target.value)}>
                {(p.options ?? []).map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            ) : p.type === 'number' ? (
              <input
                className="input"
                type="number"
                value={String(val)}
                onChange={(e) => set(p.name, e.target.value === '' ? '' : Number(e.target.value))}
              />
            ) : p.type === 'json' ? (
              <textarea
                className="input textarea"
                rows={5}
                defaultValue={asText(val)}
                placeholder={p.placeholder}
                onBlur={(e) => {
                  try {
                    set(p.name, JSON.parse(e.target.value || '{}'));
                  } catch {
                    set(p.name, e.target.value);
                  }
                }}
              />
            ) : p.type === 'expression' ? (
              <textarea
                className="input textarea"
                rows={3}
                value={String(val)}
                placeholder={p.placeholder}
                onChange={(e) => set(p.name, e.target.value)}
              />
            ) : (
              <input
                className="input"
                value={String(val)}
                placeholder={p.placeholder}
                onChange={(e) => set(p.name, e.target.value)}
              />
            )}
            {p.description && (
              <div style={{ fontSize: 11, color: 'var(--muted)', marginTop: 3 }}>{p.description}</div>
            )}
          </div>
        );
      })}

      {step && (
        <div style={{ marginTop: 20 }}>
          <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 6 }}>
            Last run · <span style={{ color: 'var(--text)' }}>{step.status}</span>
          </div>
          {step.error && (
            <pre style={errBox}>{step.error}</pre>
          )}
          <div style={{ fontSize: 11, color: 'var(--muted)' }}>Output</div>
          <pre style={ioBox}>{JSON.stringify(step.output, null, 2)}</pre>
          <div style={{ fontSize: 11, color: 'var(--muted)' }}>Input</div>
          <pre style={ioBox}>{JSON.stringify(step.input, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

const ioBox: React.CSSProperties = {
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  padding: 8,
  fontSize: 11,
  maxHeight: 160,
  overflow: 'auto',
  whiteSpace: 'pre-wrap',
};
const errBox: React.CSSProperties = { ...ioBox, color: 'var(--err)', maxHeight: 100 };
