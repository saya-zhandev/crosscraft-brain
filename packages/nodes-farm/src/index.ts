/**
 * FarmersFront vertical nodes — the produce supply-chain process modeled on the
 * crosscraft engine. Demonstrates the fork model: a vertical adds a node pack + tables,
 * and the skeleton core is untouched.
 *
 * One lot = one execution: every node keys off ctx.ids.executionId, so no cross-node
 * references are needed. Stages pause via the core "Wait for Webhook" node between these.
 */
import type { Item, Json, NodeDefinition } from '@crosscraft/schema';
import { query } from '@crosscraft/engine';

function asObj(v: unknown): Record<string, Json> {
  return v && typeof v === 'object' ? (v as Record<string, Json>) : {};
}

// --- Harvest / Start Lot (trigger): create the lot + first CTE -----------------
const startLot: NodeDefinition = {
  type: 'farm.startLot',
  label: 'Harvest — Start Lot',
  group: 'trigger',
  icon: 'Sprout',
  description: 'Webhook trigger. Creates a lot + Harvesting CTE, assigns a TLC.',
  isTrigger: true,
  inputs: [],
  outputs: [{ id: 'main' }],
  params: [{ name: 'path', label: 'Webhook path', type: 'string', default: 'start-lot' }],
  async execute(ctx) {
    const body = ctx.trigger[0]?.json ?? {};
    const commodity = String(body.commodity ?? 'LOT');
    const harvestDate = String(body.harvest_date ?? new Date().toISOString().slice(0, 10));
    const tlc = `${commodity.toUpperCase().replace(/[^A-Z]/g, '').slice(0, 4) || 'LOT'}-${harvestDate.replace(/-/g, '')}-${Math.floor(Math.random() * 90000) + 10000}`;

    const lot = await query<{ id: string }>(
      `INSERT INTO lots (farm_id, tlc, commodity, variety, harvest_date, status, execution_id)
       VALUES ($1,$2,$3,$4,$5,'harvested',$6) RETURNING id`,
      [body.farm_id ?? 1, tlc, commodity, body.variety ?? null, harvestDate, ctx.ids.executionId],
    );
    const lotId = lot.rows[0]!.id;
    await query(
      `INSERT INTO events (lot_id, stage, cte_type, kde, location, actor, photo_url, occurred_at)
       VALUES ($1,'harvest','Harvesting',$2,$3,$4,$5, COALESCE($6, now()))`,
      [lotId, JSON.stringify(asObj(body.kde) || {}), body.location ?? '', body.actor ?? '', body.photo_url ?? '', body.occurred_at ?? null],
    );
    ctx.log(`Created lot ${tlc} (#${lotId})`);
    return { outputs: { main: [{ json: { lot_id: lotId, tlc, commodity } }] } };
  },
};

// --- Record a stage CTE from a field event ------------------------------------
function recordEvent(type: string, label: string, stage: string, cte: string, icon: string): NodeDefinition {
  return {
    type,
    label,
    group: 'integration',
    icon,
    description: `Record the ${cte} CTE from the resumed field event.`,
    inputs: [{ id: 'main' }],
    outputs: [{ id: 'main' }],
    params: [],
    async execute(ctx) {
      const payload = ctx.input[0]?.json ?? {};
      const lot = await query<{ id: string }>(`SELECT id FROM lots WHERE execution_id=$1`, [ctx.ids.executionId]);
      const lotId = lot.rows[0]?.id;
      if (!lotId) throw new Error('No lot for this execution');
      await query(
        `INSERT INTO events (lot_id, stage, cte_type, kde, location, actor, photo_url, occurred_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7, COALESCE($8, now()))`,
        [lotId, stage, cte, JSON.stringify(asObj(payload.kde) ?? {}), payload.location ?? '', payload.actor ?? '', payload.photo_url ?? '', payload.occurred_at ?? null],
      );
      ctx.log(`Recorded ${cte} for lot #${lotId}`);
      return { outputs: { main: [{ json: payload } as Item] } };
    },
  };
}

// --- Close the lot ------------------------------------------------------------
const closeLot: NodeDefinition = {
  type: 'farm.closeLot',
  label: 'Close Lot (Shipped)',
  group: 'flow',
  icon: 'CheckCircle2',
  description: 'Mark the lot shipped — fully traceable.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [],
  async execute(ctx) {
    await query(`UPDATE lots SET status='shipped' WHERE execution_id=$1`, [ctx.ids.executionId]);
    return { outputs: { main: ctx.input.length ? ctx.input : [{ json: {} }] } };
  },
};

export const farmNodes: NodeDefinition[] = [
  startLot,
  recordEvent('farm.recordCooling', 'Cooling', 'cool', 'Cooling', 'Snowflake'),
  recordEvent('farm.recordPacking', 'Initial Packing', 'pack', 'Initial Packing', 'Package'),
  recordEvent('farm.recordShipping', 'Shipping', 'ship', 'Shipping', 'Truck'),
  closeLot,
];
