import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Link } from '@/components/ui/link';
import { ArrowLeft, ChevronDown, RefreshCw } from 'lucide-react';
import { api, type ExecutionRow } from '@/lib/client';
import type { StepRecord, Workflow } from '@crosscraft/schema';
import { cn } from '@/lib/utils';
import { statusBadge } from '@/lib/ui';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { JsonBlock } from '@/components/JsonBlock';
import { ResumeDialog } from '@/components/ResumeDialog';

interface Detail {
  status: string;
  waitingNodeId?: string | null;
  steps: StepRecord[];
}

export default function Executions() {
  const { workflowId = '' } = useParams();
  const [rows, setRows] = useState<ExecutionRow[] | null>(null);
  const [nodeNames, setNodeNames] = useState<Record<string, string>>({});
  const [openId, setOpenId] = useState<string | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null);

  const refreshRows = useCallback(() => api.listExecutions(workflowId).then(setRows), [workflowId]);

  useEffect(() => {
    refreshRows();
    api
      .getWorkflow(workflowId)
      .then((wf: Workflow) =>
        setNodeNames(Object.fromEntries(wf.nodes.map((n) => [n.id, n.name || n.type]))),
      )
      .catch(() => {});
  }, [workflowId, refreshRows]);

  const open = useCallback(async (id: string) => {
    setOpenId(id);
    setDetail(null);
    const d = (await fetch(`/api/executions/${id}`).then((r) => r.json())) as Detail;
    setDetail(d);
  }, []);

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-border bg-bg/80 px-6 py-3 backdrop-blur">
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to editor">
          <Link href={`/editor/${workflowId}`}>
            <ArrowLeft />
          </Link>
        </Button>
        <h1 className="text-base font-semibold">Runs</h1>
        <span className="hidden text-sm text-muted sm:inline">
          Transparent monitoring — every node’s input &amp; output.
        </span>
        <div className="flex-1" />
        <Button variant="ghost" size="sm" onClick={refreshRows}>
          <RefreshCw />
          Refresh
        </Button>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-6">
        <div className={cn('grid gap-5', openId ? 'lg:grid-cols-[320px_1fr]' : 'grid-cols-1')}>
          {/* run list */}
          <div className="flex flex-col gap-2">
            {rows === null ? (
              Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-14" />)
            ) : rows.length === 0 ? (
              <p className="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted">
                No runs yet. Open the editor and hit Run.
              </p>
            ) : (
              rows.map((r) => (
                <button
                  key={r.id}
                  onClick={() => open(r.id)}
                  className={cn(
                    'rounded-lg border px-3 py-2.5 text-left transition-colors',
                    openId === r.id
                      ? 'border-border-2 bg-panel-2'
                      : 'border-border bg-panel hover:border-border-2 hover:bg-panel-2',
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-[12px] text-muted">{r.id.slice(0, 10)}</span>
                    <Badge variant={statusBadge(r.status)}>{r.status}</Badge>
                  </div>
                  <div className="mt-1 text-[11px] text-muted">{new Date(r.startedAt).toLocaleString()}</div>
                </button>
              ))
            )}
          </div>

          {/* run detail */}
          {openId && (
            <div>
              {!detail ? (
                <div className="space-y-3">
                  <Skeleton className="h-8 w-40" />
                  <Skeleton className="h-28" />
                  <Skeleton className="h-28" />
                </div>
              ) : (
                <div className="space-y-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <span className="text-sm text-muted">Status</span>
                    <Badge variant={statusBadge(detail.status)}>{detail.status}</Badge>
                    {detail.status === 'waiting' && (
                      <ResumeDialog executionId={openId} onResumed={() => open(openId)} />
                    )}
                  </div>

                  {detail.steps.length === 0 && (
                    <p className="text-sm text-muted">No steps recorded.</p>
                  )}

                  {detail.steps.map((s) => (
                    <StepCard key={s.id} step={s} title={nodeNames[s.nodeId] ?? s.nodeId} />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function StepCard({ step, title }: { step: StepRecord; title: string }) {
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-2.5">
        <span className="truncate text-[13px] font-semibold">{title}</span>
        <Badge variant={statusBadge(step.status)}>{step.status}</Badge>
      </div>
      <div className="space-y-3 p-4">
        {step.error && <JsonBlock value={step.error} tone="error" />}

        <Labelled label="Output">
          <JsonBlock value={step.output} />
        </Labelled>

        <Collapsible>
          <CollapsibleTrigger className="group flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wide text-muted hover:text-text">
            <ChevronDown className="size-3.5 transition-transform group-data-[state=open]:rotate-180" />
            Input{step.logs?.length ? ' & logs' : ''}
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-2 space-y-2">
            <JsonBlock value={step.input} />
            {step.logs?.map((l, i) => (
              <div key={i} className="rounded-md border border-border bg-bg px-2.5 py-1.5 text-[11px]">
                <span className="text-text">{l.message}</span>
                {l.data !== undefined && (
                  <span className="ml-2 font-mono text-muted">{JSON.stringify(l.data)}</span>
                )}
              </div>
            ))}
          </CollapsibleContent>
        </Collapsible>
      </div>
    </Card>
  );
}

function Labelled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{label}</div>
      {children}
    </div>
  );
}
