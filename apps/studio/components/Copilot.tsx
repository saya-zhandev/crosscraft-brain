'use client';
import { useEffect, useRef, useState } from 'react';
import { Sparkles, X, Send } from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/engine';
import type { GraphOp, Workflow } from '@crosscraft/schema';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

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
    {
      role: 'assistant',
      text: 'Describe a workflow and I’ll build it on the canvas. e.g. “When a webhook arrives, summarize the text with AI, then branch if urgent.”',
    },
  ]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
  }, [msgs, busy]);

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
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="flex items-center gap-2 text-sm font-semibold">
          <Sparkles className="size-4 text-accent-2" />
          Copilot
        </span>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close copilot">
          <X />
        </Button>
      </div>

      <div ref={scrollRef} className="flex flex-1 flex-col gap-2.5 overflow-y-auto p-4">
        {msgs.map((m, i) => (
          <div
            key={i}
            className={cn(
              'max-w-[90%] whitespace-pre-wrap rounded-xl px-3 py-2 text-[13px] leading-relaxed',
              m.role === 'user'
                ? 'self-end bg-accent text-accent-fg'
                : 'self-start border border-border bg-panel-2 text-text',
            )}
          >
            {m.text}
          </div>
        ))}
        {busy && (
          <div className="flex items-center gap-1.5 self-start rounded-xl border border-border bg-panel-2 px-3 py-2.5">
            <Dot /> <Dot delay={150} /> <Dot delay={300} />
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 border-t border-border p-3">
        <Input
          placeholder="Describe a workflow…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && send()}
        />
        <Button size="icon" onClick={send} disabled={busy || !input.trim()} aria-label="Send">
          <Send />
        </Button>
      </div>
    </div>
  );
}

function Dot({ delay = 0 }: { delay?: number }) {
  return (
    <span
      className="size-1.5 animate-bounce rounded-full bg-muted"
      style={{ animationDelay: `${delay}ms` }}
    />
  );
}
