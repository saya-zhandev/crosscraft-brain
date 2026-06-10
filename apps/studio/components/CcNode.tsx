'use client';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import * as Icons from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/engine';
import type { StepStatus } from '@crosscraft/schema';

const GROUP_COLOR: Record<string, string> = {
  trigger: '#22c55e',
  transform: '#6366f1',
  flow: '#f59e0b',
  integration: '#38bdf8',
  ai: '#a855f7',
};

const STATUS_BORDER: Record<string, string> = {
  running: '#f59e0b',
  success: '#22c55e',
  error: '#ef4444',
  waiting: '#38bdf8',
};

export interface CcNodeData {
  label: string;
  descriptor: NodeDescriptor;
  params: Record<string, unknown>;
  status?: StepStatus | 'waiting';
  [key: string]: unknown;
}

export function CcNode({ data, selected }: NodeProps) {
  const d = data as CcNodeData;
  const desc = d.descriptor;
  const accent = GROUP_COLOR[desc.group] ?? '#6366f1';
  const border = d.status ? STATUS_BORDER[d.status] : selected ? '#818cf8' : 'var(--border)';
  const Icon = (desc.icon && (Icons as Record<string, unknown>)[desc.icon]) as
    | React.ComponentType<{ size?: number; color?: string }>
    | undefined;

  return (
    <div
      style={{
        minWidth: 180,
        background: 'var(--panel-2)',
        border: `2px solid ${border}`,
        borderRadius: 12,
        boxShadow: selected ? '0 0 0 3px rgba(129,140,248,.25)' : 'none',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '10px 12px' }}>
        <span style={{ width: 26, height: 26, borderRadius: 7, background: accent + '22', display: 'grid', placeItems: 'center' }}>
          {Icon ? <Icon size={15} color={accent} /> : <span style={{ color: accent }}>●</span>}
        </span>
        <div style={{ overflow: 'hidden' }}>
          <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap' }}>{d.label}</div>
          <div style={{ fontSize: 10, color: 'var(--muted)' }}>{desc.label}</div>
        </div>
      </div>

      {/* input handle */}
      {desc.inputs.map((p) => (
        <Handle key={`in-${p.id}`} id={p.id} type="target" position={Position.Left} style={{ top: 24 }} />
      ))}
      {/* output handles, stacked when multiple (e.g. if -> true/false) */}
      {desc.outputs.map((p, i) => (
        <Handle
          key={`out-${p.id}`}
          id={p.id}
          type="source"
          position={Position.Right}
          style={{ top: 24 + i * 20 }}
        >
          {desc.outputs.length > 1 && (
            <span style={{ position: 'absolute', right: 12, top: -8, fontSize: 9, color: 'var(--muted)' }}>
              {p.label ?? p.id}
            </span>
          )}
        </Handle>
      ))}
    </div>
  );
}
