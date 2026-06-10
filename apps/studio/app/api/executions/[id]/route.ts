import { NextResponse } from 'next/server';
import { loadExecution, getExecutionSteps } from '@crosscraft/engine';

// Full run detail for Transparent Monitoring: status + every node's input/output.
export async function GET(_req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const exec = await loadExecution(id);
  if (!exec) return NextResponse.json({ error: 'not found' }, { status: 404 });
  const steps = await getExecutionSteps(id);
  return NextResponse.json({
    id: exec.id,
    workflowId: exec.workflowId,
    status: exec.status,
    waitingNodeId: exec.waitingNodeId,
    startedAt: exec.startedAt,
    finishedAt: exec.finishedAt,
    steps,
  });
}
