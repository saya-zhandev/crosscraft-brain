import { NextResponse } from 'next/server';
import { getWorkflow, run } from '@crosscraft/engine';
import type { Item } from '@crosscraft/schema';
import { registry } from '@/lib/registry';

// Manual run (the "Run" button). Optional JSON body becomes the trigger item.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const wf = await getWorkflow(id);
  if (!wf) return NextResponse.json({ error: 'not found' }, { status: 404 });

  let triggerItems: Item[] = [{ json: {} }];
  try {
    const body = await req.json();
    if (body && typeof body === 'object') triggerItems = [{ json: body }];
  } catch {
    /* no body */
  }

  const result = await run(wf, registry(), { triggerItems });
  return NextResponse.json(result);
}
