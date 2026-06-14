'use client';
import type { StepRecord } from '@crosscraft/schema';
import { Badge } from '@/components/ui/badge';
import { statusBadge } from '@/lib/ui';
import { JsonBlock } from './JsonBlock';

/** The "Run" tab for the selected node — its last execution's status + I/O + logs. */
export function NodeRunOutput({ step }: { step?: StepRecord }) {
  if (!step) {
    return (
      <div className="grid h-full place-items-center p-6 text-center text-sm text-muted">
        Run the workflow to see this node’s input, output and logs here.
      </div>
    );
  }
  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted">Last run</span>
        <Badge variant={statusBadge(step.status)}>{step.status}</Badge>
      </div>

      {step.error && (
        <Section label="Error">
          <JsonBlock value={step.error} tone="error" />
        </Section>
      )}

      <Section label="Output">
        <JsonBlock value={step.output} />
      </Section>

      <Section label="Input">
        <JsonBlock value={step.input} />
      </Section>

      {step.logs && step.logs.length > 0 && (
        <Section label="Logs">
          <div className="space-y-1">
            {step.logs.map((l, i) => (
              <div key={i} className="rounded-md border border-border bg-bg px-2.5 py-1.5 text-[11px]">
                <span className="text-text">{l.message}</span>
                {l.data !== undefined && (
                  <span className="ml-2 font-mono text-muted">{JSON.stringify(l.data)}</span>
                )}
              </div>
            ))}
          </div>
        </Section>
      )}
    </div>
  );
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted">{label}</div>
      {children}
    </div>
  );
}
