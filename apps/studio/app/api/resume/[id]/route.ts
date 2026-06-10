import { NextResponse } from 'next/server';
import { getWorkflow, resume } from '@crosscraft/engine';
import { registry } from '@/lib/registry';

// Resume a waiting execution. The posted body becomes the resumed node's output item.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const body = await req.json().catch(() => ({}));
  try {
    const result = await resume(id, registry(), [{ json: body }], getWorkflow);
    if (result.status === 'waiting' && result.respond) {
      return NextResponse.json(
        { executionId: result.executionId, ...((result.respond.body as object) ?? {}) },
        { status: result.respond.status ?? 200 },
      );
    }
    return NextResponse.json(result);
  } catch (e) {
    return NextResponse.json({ error: (e as Error).message }, { status: 400 });
  }
}
