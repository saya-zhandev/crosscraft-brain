'use client';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import * as Icons from 'lucide-react';
import { Loader2 } from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/schema';
import type { StepStatus } from '@crosscraft/schema';
import { cn } from '@/lib/utils';
import { groupVar, statusVar } from '@/lib/ui';

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
  const accent = groupVar(desc.group);
  const Icon = (desc.icon && (Icons as Record<string, unknown>)[desc.icon]) as
    | React.ComponentType<{ size?: number; color?: string }>
    | undefined;

  // Border: live run status wins, else selection, else hairline. Token-driven.
  const borderColor = d.status
    ? statusVar(d.status)
    : selected
      ? 'var(--accent-2)'
      : 'var(--border)';

  return (
    <div
      className={cn(
        'min-w-[180px] rounded-xl bg-panel-2 transition-shadow',
        selected && 'shadow-[0_0_0_3px_color-mix(in_srgb,var(--accent-2)_25%,transparent)]',
      )}
      style={{ border: `2px solid ${borderColor}` }}
    >
      <div className="flex items-center gap-2.5 px-3 py-2.5">
        <span
          className="grid size-7 shrink-0 place-items-center rounded-lg"
          style={{ background: `color-mix(in srgb, ${accent} 16%, transparent)` }}
        >
          {Icon ? <Icon size={15} color={accent} /> : <span style={{ color: accent }}>●</span>}
        </span>
        <div className="min-w-0">
          <div className="truncate text-[13px] font-semibold leading-tight">{d.label}</div>
          <div className="truncate text-[10px] text-muted">{desc.label}</div>
        </div>
        {d.status === 'running' && <Loader2 className="ml-auto size-3.5 animate-spin text-warn" />}
        {d.status === 'waiting' && <span className="ml-auto size-2 rounded-full bg-wait" />}
      </div>

      {/* input handles */}
      {desc.inputs.map((p) => (
        <Handle key={`in-${p.id}`} id={p.id} type="target" position={Position.Left} style={{ top: 26 }} />
      ))}

      {/* output handles, stacked + labelled when multiple (e.g. if -> true/false) */}
      {desc.outputs.map((p, i) => (
        <Handle key={`out-${p.id}`} id={p.id} type="source" position={Position.Right} style={{ top: 26 + i * 20 }}>
          {desc.outputs.length > 1 && (
            <span className="pointer-events-none absolute -top-2 right-3 text-[9px] text-muted">
              {p.label ?? p.id}
            </span>
          )}
        </Handle>
      ))}
    </div>
  );
}
