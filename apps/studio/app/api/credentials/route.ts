import { NextResponse } from 'next/server';
import { nanoid } from 'nanoid';
import { query, encrypt } from '@crosscraft/engine';

// Credentials are encrypted at rest (AES-256-GCM). The `data` (api keys, db passwords)
// is never returned by the API — only id/type/name.
export async function GET() {
  const r = await query<{ id: string; type: string; name: string }>(
    `SELECT id, type, name FROM credentials ORDER BY created_at DESC`,
  );
  return NextResponse.json(r.rows);
}

export async function POST(req: Request) {
  const { type, name, data } = (await req.json()) as {
    type: string;
    name: string;
    data: Record<string, unknown>;
  };
  if (!type || !name) return NextResponse.json({ error: 'type and name required' }, { status: 400 });
  const id = nanoid();
  await query(`INSERT INTO credentials (id, type, name, data_encrypted) VALUES ($1,$2,$3,$4)`, [
    id,
    type,
    name,
    encrypt(JSON.stringify(data ?? {})),
  ]);
  return NextResponse.json({ id, type, name });
}
