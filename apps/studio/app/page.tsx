'use client';
import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { Plus, Workflow as WorkflowIcon, History, Search } from 'lucide-react';
import { api } from '@/lib/client';
import type { Workflow } from '@crosscraft/schema';
import { brand } from '@/lib/brand';
import { AppHeader } from '@/components/AppHeader';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';

type WfRow = Pick<Workflow, 'id' | 'name' | 'active'>;

export default function Home() {
  const [wfs, setWfs] = useState<WfRow[]>([]);
  const [q, setQ] = useState('');
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    api
      .listWorkflows()
      .then(setWfs)
      .finally(() => setLoading(false));
  }, []);

  const create = async () => {
    setCreating(true);
    try {
      const wf = await api.createWorkflow(name.trim() || 'Untitled workflow');
      location.href = `/editor/${wf.id}`;
    } finally {
      setCreating(false);
    }
  };

  const filtered = useMemo(() => {
    const n = q.trim().toLowerCase();
    return n ? wfs.filter((w) => w.name.toLowerCase().includes(n)) : wfs;
  }, [wfs, q]);

  return (
    <div className="min-h-screen">
      <AppHeader />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold">Workflows</h1>
            <p className="mt-1 text-sm text-muted">{brand.tagline}</p>
          </div>
          <CreateDialog
            open={createOpen}
            onOpenChange={setCreateOpen}
            name={name}
            setName={setName}
            creating={creating}
            onCreate={create}
          />
        </div>

        <div className="relative mb-5 max-w-sm">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search workflows…"
            className="pl-9"
          />
        </div>

        {loading ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-24" />
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState hasAny={wfs.length > 0} onCreate={() => setCreateOpen(true)} />
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {filtered.map((w) => (
              <Card key={w.id} className="group relative transition-colors hover:border-border-2">
                <Link href={`/editor/${w.id}`} className="block p-4">
                  <div className="mb-3 flex items-start justify-between gap-2">
                    <span className="grid size-9 place-items-center rounded-lg bg-accent/15 text-accent-2">
                      <WorkflowIcon className="size-4.5" />
                    </span>
                    <Badge variant={w.active ? 'success' : 'secondary'}>
                      {w.active ? 'Active' : 'Inactive'}
                    </Badge>
                  </div>
                  <div className="truncate text-[15px] font-semibold">{w.name}</div>
                  <div className="mt-0.5 truncate font-mono text-[11px] text-muted">{w.id}</div>
                </Link>
                <div className="flex items-center gap-1 border-t border-border px-3 py-2">
                  <Button asChild variant="ghost" size="sm" className="text-muted">
                    <Link href={`/executions/${w.id}`}>
                      <History />
                      Runs
                    </Link>
                  </Button>
                  <div className="flex-1" />
                  <Button asChild variant="ghost" size="sm">
                    <Link href={`/editor/${w.id}`}>Open →</Link>
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        )}
      </main>
    </div>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  name,
  setName,
  creating,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  name: string;
  setName: (v: string) => void;
  creating: boolean;
  onCreate: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button>
          <Plus />
          New workflow
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create workflow</DialogTitle>
        </DialogHeader>
        <Input
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onCreate()}
          placeholder="Workflow name"
        />
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={onCreate} disabled={creating}>
            Create & open
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EmptyState({ hasAny, onCreate }: { hasAny: boolean; onCreate: () => void }) {
  return (
    <div className="grid place-items-center rounded-xl border border-dashed border-border bg-panel/40 py-20 text-center">
      <div className="flex max-w-sm flex-col items-center gap-3">
        <div className="grid size-12 place-items-center rounded-2xl bg-accent/15 text-accent-2">
          <WorkflowIcon className="size-6" />
        </div>
        <div>
          <p className="text-sm font-semibold">{hasAny ? 'No matches' : 'No workflows yet'}</p>
          <p className="mt-1 text-[13px] text-muted">
            {hasAny ? 'Try a different search.' : 'Create your first automation to get started.'}
          </p>
        </div>
        {!hasAny && (
          <Button onClick={onCreate}>
            <Plus />
            New workflow
          </Button>
        )}
      </div>
    </div>
  );
}
