import { NextResponse } from 'next/server';
import { query } from '@crosscraft/engine';

// Delete a credential by id. (Secrets are encrypted at rest and never returned by the API.)
export async function DELETE(_req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  await query(`DELETE FROM credentials WHERE id = $1`, [id]);
  return NextResponse.json({ ok: true });
}
