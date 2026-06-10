'use client';
import * as Icons from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/engine';

const GROUPS: { key: string; label: string }[] = [
  { key: 'trigger', label: 'Triggers' },
  { key: 'transform', label: 'Transform' },
  { key: 'flow', label: 'Flow' },
  { key: 'integration', label: 'Integrations' },
  { key: 'ai', label: 'AI' },
];

export function Palette({ descriptors }: { descriptors: NodeDescriptor[] }) {
  return (
    <div style={{ width: 220, borderRight: '1px solid var(--border)', overflowY: 'auto', padding: 12 }}>
      <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 8 }}>
        Drag a node onto the canvas
      </div>
      {GROUPS.map((g) => {
        const items = descriptors.filter((d) => d.group === g.key);
        if (!items.length) return null;
        return (
          <div key={g.key} style={{ marginBottom: 16 }}>
            <div style={{ fontSize: 11, textTransform: 'uppercase', color: 'var(--muted)', margin: '6px 0' }}>
              {g.label}
            </div>
            {items.map((d) => {
              const Icon = (d.icon && (Icons as Record<string, unknown>)[d.icon]) as
                | React.ComponentType<{ size?: number }>
                | undefined;
              return (
                <div
                  key={d.type}
                  draggable
                  onDragStart={(e) => e.dataTransfer.setData('application/cc-node', d.type)}
                  title={d.description}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '8px 10px',
                    marginBottom: 6,
                    background: 'var(--panel-2)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    cursor: 'grab',
                    fontSize: 13,
                  }}
                >
                  {Icon ? <Icon size={15} /> : <span>●</span>}
                  {d.label}
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
