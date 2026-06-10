import { query } from '@crosscraft/engine';

// Trace report for a lot, built from the executed graph's events.
// Returns HTML; rendering to PDF is the same Gotenberg step proven in farmersback.
export async function POST(req: Request) {
  const { executionId, tlc } = (await req.json().catch(() => ({}))) as {
    executionId?: string;
    tlc?: string;
  };

  const lotRes = await query<{
    id: string; tlc: string; commodity: string; variety: string | null; status: string; farm_name: string;
  }>(
    `SELECT l.id, l.tlc, l.commodity, l.variety, l.status, f.name AS farm_name
     FROM lots l JOIN farms f ON f.id = l.farm_id
     WHERE ($1::text IS NOT NULL AND l.execution_id = $1) OR ($1::text IS NULL AND l.tlc = $2)
     LIMIT 1`,
    [executionId ?? null, tlc ?? null],
  );
  const lot = lotRes.rows[0];
  if (!lot) return new Response('Lot not found', { status: 404 });

  const ev = await query<{
    stage: string; cte_type: string; kde: Record<string, unknown>; location: string; actor: string; occurred_at: string;
  }>(`SELECT stage, cte_type, kde, location, actor, occurred_at FROM events WHERE lot_id=$1 ORDER BY occurred_at`, [lot.id]);

  const esc = (s: unknown) => String(s ?? '').replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]!));
  const stages = ['harvest', 'cool', 'pack', 'ship'];
  const colors: Record<string, string> = { harvest: '#22c55e', cool: '#3b82f6', pack: '#f59e0b', ship: '#a855f7' };
  const byStage = new Map(ev.rows.map((e) => [e.stage, e]));
  const chain = stages
    .map((s) => {
      const done = byStage.has(s);
      return `<div style="border:2px solid ${colors[s]};opacity:${done ? 1 : 0.3};border-radius:10px;padding:10px 14px;text-align:center;min-width:90px"><b>${s[0]!.toUpperCase() + s.slice(1)}</b><div style="font-size:10px;color:#555">${done ? new Date(byStage.get(s)!.occurred_at).toLocaleString() : 'pending'}</div></div>`;
    })
    .join('<div style="color:#94a3b8">&rarr;</div>');
  const rows = ev.rows
    .map(
      (e) =>
        `<tr><td>${esc(e.cte_type)}</td><td>${e.occurred_at ? new Date(e.occurred_at).toLocaleString() : ''}</td><td>${esc(e.location)}</td><td>${esc(e.actor)}</td><td>${Object.entries(e.kde || {}).map(([k, v]) => `<b>${esc(k)}</b>: ${esc(v)}`).join('<br>')}</td></tr>`,
    )
    .join('');

  const html = `<!doctype html><meta charset="utf-8"><body style="font-family:Arial,sans-serif;margin:32px;color:#111">
<h1>Traceability Report</h1>
<div style="color:#555">${esc(lot.farm_name)} &middot; ${esc(lot.commodity)}${lot.variety ? ` (${esc(lot.variety)})` : ''}</div>
<div style="background:#f1f5f9;display:inline-block;padding:4px 10px;border-radius:6px;margin:8px 0">TLC: ${esc(lot.tlc)}</div>
<div style="display:flex;align-items:center;gap:8px;margin:20px 0">${chain}</div>
<table style="width:100%;border-collapse:collapse;font-size:12px" border="1" cellpadding="8">
<thead><tr style="background:#f8fafc"><th>CTE</th><th>When</th><th>Location</th><th>Actor</th><th>KDEs</th></tr></thead>
<tbody>${rows || '<tr><td colspan="5">No events.</td></tr>'}</tbody></table>
<p style="font-size:10px;color:#777;margin-top:24px">Generated ${new Date().toLocaleString()} &middot; FarmersFront on crosscraft &middot; FDA FSMA 204 CTEs.</p>
</body>`;

  return new Response(html, { headers: { 'content-type': 'text/html; charset=utf-8' } });
}
