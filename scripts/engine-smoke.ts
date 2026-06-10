/**
 * Engine smoke test (no UI): proves topological run, per-step persistence, and the
 * durable suspend/resume primitive — the owned version of farmersback's webhook-wait.
 *
 * Run:  node --env-file=.env --import tsx scripts/engine-smoke.ts
 */
import { Registry, run, resume, saveWorkflow, getWorkflow, loadExecution, getExecutionSteps, closePool } from '@crosscraft/engine';
import { coreNodes } from '@crosscraft/nodes-core';
import type { Workflow } from '@crosscraft/schema';

const registry = new Registry().register(...coreNodes);

const wf: Workflow = {
  id: 'smoke-wf',
  name: 'Smoke: webhook -> set -> wait -> set',
  active: true,
  nodes: [
    { id: 'trig', type: 'core.webhookTrigger', params: { path: 'smoke' }, position: { x: 0, y: 0 } },
    { id: 'tag', type: 'core.set', params: { fields: { stage: 'started', lot: '{{ $json.lot }}' } }, position: { x: 1, y: 0 } },
    { id: 'hold', type: 'core.wait', params: {}, position: { x: 2, y: 0 } },
    { id: 'done', type: 'core.set', params: { fields: { stage: 'resumed', temp: '{{ $json.temp_f }}', lot: '{{ $node("tag")[0].json.lot }}' } }, position: { x: 3, y: 0 } },
  ],
  edges: [
    { id: 'e1', source: 'trig', target: 'tag' },
    { id: 'e2', source: 'tag', target: 'hold' },
    { id: 'e3', source: 'hold', target: 'done' },
  ],
};

function assert(cond: unknown, msg: string) {
  if (!cond) throw new Error('ASSERT FAILED: ' + msg);
}

async function main() {
  await saveWorkflow(wf);

  console.log('1) start run (webhook payload)...');
  const r1 = await run(wf, registry, { triggerItems: [{ json: { lot: 'L-001' } }] });
  console.log('   ->', r1.status, 'exec', r1.executionId);
  assert(r1.status === 'waiting', 'run should suspend at wait node');

  const ex1 = await loadExecution(r1.executionId);
  assert(ex1?.status === 'waiting' && ex1.waitingNodeId === 'hold', 'execution waiting at hold');

  console.log('2) resume with field payload...');
  const r2 = await resume(r1.executionId, registry, [{ json: { temp_f: 34 } }], getWorkflow);
  console.log('   ->', r2.status);
  assert(r2.status === 'success', 'resumed run should succeed');

  console.log('3) inspect persisted steps (monitoring data)...');
  const steps = await getExecutionSteps(r1.executionId);
  for (const s of steps) {
    console.log(`   ${s.nodeId.padEnd(6)} ${s.status.padEnd(8)} out=${JSON.stringify(s.output)}`);
  }
  const doneStep = steps.find((s) => s.nodeId === 'done');
  assert(doneStep, 'done step recorded');
  const out = doneStep!.output[0]?.json as Record<string, unknown> | undefined;
  assert(out?.stage === 'resumed', 'final set ran after resume');
  assert(out?.temp === 34 || out?.temp === '34', 'resume payload flowed into final node');
  assert(out?.lot === 'L-001', 'trigger data preserved through the chain');

  console.log('\nPASS: run -> suspend -> resume -> success, with full step I/O persisted.');
  await closePool();
}

main().catch(async (e) => {
  console.error(e);
  await closePool();
  process.exit(1);
});
