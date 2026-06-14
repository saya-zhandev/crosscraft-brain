import { Loader2, CheckCircle2, XCircle, PauseCircle, Circle } from 'lucide-react';
import { cn } from '@/lib/utils';

type Pill = { label: string; cls: string; icon: React.ReactNode };

const IDLE: Pill = { label: 'Idle', cls: 'text-muted', icon: <Circle className="size-3.5" /> };
const MAP: Record<string, Pill> = {
  idle: IDLE,
  saving: { label: 'Saving…', cls: 'text-muted', icon: <Loader2 className="size-3.5 animate-spin" /> },
  running: { label: 'Running', cls: 'text-warn', icon: <Loader2 className="size-3.5 animate-spin" /> },
  waiting: { label: 'Waiting', cls: 'text-wait', icon: <PauseCircle className="size-3.5" /> },
  success: { label: 'Success', cls: 'text-ok', icon: <CheckCircle2 className="size-3.5" /> },
  error: { label: 'Error', cls: 'text-err', icon: <XCircle className="size-3.5" /> },
};

export function RunStatusPill({ status }: { status: string }) {
  const s = MAP[status] ?? IDLE;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border border-border bg-panel-2 px-2.5 py-1 text-xs font-medium',
        s.cls,
      )}
    >
      {s.icon}
      {s.label}
    </span>
  );
}
