'use client';
import { useMemo, useState } from 'react';
import * as Icons from 'lucide-react';
import { Star } from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/schema';
import { cn } from '@/lib/utils';
import { groupVar } from '@/lib/ui';
import { Input } from '@/components/ui/input';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { ScrollArea } from '@/components/ui/scroll-area';

const GROUPS: { key: string; label: string }[] = [
  { key: 'trigger', label: 'Triggers' },
  { key: 'transform', label: 'Transform' },
  { key: 'flow', label: 'Flow' },
  { key: 'integration', label: 'Integrations' },
  { key: 'ai', label: 'AI' },
];

const FAV_KEY = 'cc.palette.favorites';

function lucide(name?: string) {
  return (name && (Icons as Record<string, unknown>)[name]) as
    | React.ComponentType<{ size?: number; color?: string; className?: string }>
    | undefined;
}

export function Palette({
  descriptors,
  onAdd,
}: {
  descriptors: NodeDescriptor[];
  onAdd?: (type: string) => void;
}) {
  const [q, setQ] = useState('');
  const [favs, setFavs] = useState<string[]>(() => {
    if (typeof window === 'undefined') return [];
    try {
      return JSON.parse(localStorage.getItem(FAV_KEY) || '[]') as string[];
    } catch {
      return [];
    }
  });

  const toggleFav = (type: string) => {
    setFavs((cur) => {
      const next = cur.includes(type) ? cur.filter((t) => t !== type) : [...cur, type];
      localStorage.setItem(FAV_KEY, JSON.stringify(next));
      return next;
    });
  };

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return descriptors;
    return descriptors.filter((d) =>
      [d.label, d.description, d.type].some((s) => s?.toLowerCase().includes(needle)),
    );
  }, [descriptors, q]);

  const favItems = filtered.filter((d) => favs.includes(d.type));

  return (
    <div className="flex w-60 shrink-0 flex-col border-r border-border bg-panel/40">
      <div className="border-b border-border p-3">
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search nodes…"
          className="h-8"
        />
      </div>
      <ScrollArea className="flex-1">
        <div className="p-3">
          {filtered.length === 0 && (
            <p className="px-1 py-6 text-center text-sm text-muted">No nodes match “{q}”.</p>
          )}

          {favItems.length > 0 && (
            <Group label="Favorites" items={favItems} favs={favs} onToggleFav={toggleFav} onAdd={onAdd} />
          )}

          {GROUPS.map((g) => {
            const items = filtered.filter((d) => d.group === g.key);
            if (!items.length) return null;
            return (
              <Group
                key={g.key}
                label={g.label}
                items={items}
                favs={favs}
                onToggleFav={toggleFav}
                onAdd={onAdd}
              />
            );
          })}
        </div>
      </ScrollArea>
      <div className="border-t border-border px-3 py-2 text-[11px] text-muted">
        Drag onto the canvas, or click to add.
      </div>
    </div>
  );
}

function Group({
  label,
  items,
  favs,
  onToggleFav,
  onAdd,
}: {
  label: string;
  items: NodeDescriptor[];
  favs: string[];
  onToggleFav: (type: string) => void;
  onAdd?: (type: string) => void;
}) {
  return (
    <div className="mb-4">
      <div className="mb-1.5 px-1 text-[11px] font-semibold uppercase tracking-wide text-muted">
        {label}
      </div>
      <div className="flex flex-col gap-1">
        {items.map((d) => {
          const Icon = lucide(d.icon);
          const accent = groupVar(d.group);
          const isFav = favs.includes(d.type);
          return (
            <Tooltip key={d.type}>
              <TooltipTrigger asChild>
                <div
                  draggable
                  onDragStart={(e) => e.dataTransfer.setData('application/cc-node', d.type)}
                  onClick={() => onAdd?.(d.type)}
                  className={cn(
                    'group flex cursor-grab items-center gap-2.5 rounded-md border border-border bg-panel-2 px-2.5 py-2 text-[13px]',
                    'transition-colors hover:border-border-2 hover:bg-panel-3 active:cursor-grabbing',
                  )}
                >
                  <span
                    className="grid size-6 shrink-0 place-items-center rounded-md"
                    style={{ background: `color-mix(in srgb, ${accent} 16%, transparent)` }}
                  >
                    {Icon ? <Icon size={14} color={accent} /> : <span style={{ color: accent }}>●</span>}
                  </span>
                  <span className="flex-1 truncate">{d.label}</span>
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onToggleFav(d.type);
                    }}
                    className={cn(
                      'shrink-0 rounded p-0.5 text-muted opacity-0 transition-opacity hover:text-warn group-hover:opacity-100',
                      isFav && 'opacity-100 text-warn',
                    )}
                    aria-label={isFav ? 'Unfavorite' : 'Favorite'}
                  >
                    <Star size={13} fill={isFav ? 'currentColor' : 'none'} />
                  </button>
                </div>
              </TooltipTrigger>
              {d.description && (
                <TooltipContent side="right">
                  <p className="font-semibold">{d.label}</p>
                  <p className="text-muted">{d.description}</p>
                </TooltipContent>
              )}
            </Tooltip>
          );
        })}
      </div>
    </div>
  );
}
