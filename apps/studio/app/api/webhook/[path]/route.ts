import { NextResponse } from 'next/server';
import { listWorkflows, getWorkflow, run } from '@crosscraft/engine';
import { registry } from '@/lib/registry';

// Public webhook ingress: find the active workflow whose webhook trigger has this path,
// start a run with the posted body. Generalizes farmersback's /webhook/{path}.
export async function POST(req: Request, { params }: { params: Promise<{ path: string }> }) {
  const { path } = await params;
  const body = await req.json().catch(() => ({}));

  for (const meta of await listWorkflows()) {
    const wf = await getWorkflow(meta.id);
    if (!wf || !wf.active) continue;
    // Match any trigger node (core or a vertical's) that declares this webhook path.
    const trigger = wf.nodes.find((n) => (n.params as { path?: string })?.path === path);
    if (!trigger) continue;

    const result = await run(wf, registry(), { triggerItems: [{ json: body }] });
    if (result.status === 'waiting' && result.respond) {
      return NextResponse.json(
        { executionId: result.executionId, ...((result.respond.body as object) ?? {}) },
        { status: result.respond.status ?? 200 },
      );
    }
    return NextResponse.json(result);
  }
  return NextResponse.json({ error: `no active workflow for webhook "${path}"` }, { status: 404 });
}
