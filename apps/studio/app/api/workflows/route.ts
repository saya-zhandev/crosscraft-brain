import { NextResponse } from 'next/server';
import { nanoid } from 'nanoid';
import { listWorkflows, saveWorkflow } from '@crosscraft/engine';
import type { Workflow } from '@crosscraft/schema';

export async function GET() {
  return NextResponse.json(await listWorkflows());
}

export async function POST(req: Request) {
  const body = (await req.json()) as Partial<Workflow>;
  const wf: Workflow = {
    id: body.id ?? nanoid(),
    name: body.name ?? 'Untitled workflow',
    active: body.active ?? false,
    nodes: body.nodes ?? [],
    edges: body.edges ?? [],
    settings: body.settings ?? {},
  };
  await saveWorkflow(wf);
  return NextResponse.json(wf);
}
