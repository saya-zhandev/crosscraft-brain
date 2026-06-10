import { NextResponse } from 'next/server';
import { getWorkflow, saveWorkflow } from '@crosscraft/engine';
import type { Workflow } from '@crosscraft/schema';

export async function GET(_req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const wf = await getWorkflow(id);
  if (!wf) return NextResponse.json({ error: 'not found' }, { status: 404 });
  return NextResponse.json(wf);
}

export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const body = (await req.json()) as Workflow;
  const wf: Workflow = { ...body, id };
  await saveWorkflow(wf);
  return NextResponse.json(wf);
}
