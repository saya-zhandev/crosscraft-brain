import { NextResponse } from 'next/server';
import { listExecutions } from '@crosscraft/engine';

export async function GET(req: Request) {
  const url = new URL(req.url);
  const workflowId = url.searchParams.get('workflowId') ?? undefined;
  return NextResponse.json(await listExecutions(workflowId));
}
