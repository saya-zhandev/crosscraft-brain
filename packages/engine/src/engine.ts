/**
 * Execution engine — topological run with durable suspend/resume.
 *
 * A run walks the graph from its trigger, executing each node when all its upstream
 * sources are done. Node I/O is persisted per step (powers monitoring). A node may
 * suspend the run (durable wait); state is saved to Postgres and the run resumes when
 * an external call hits /api/resume/{executionId}. This is the owned generalization of
 * the webhook-wait pattern proven in farmersback.
 */
import { nanoid } from 'nanoid';
import { DEFAULT_PORT } from '@crosscraft/schema';
import type {
  ExecContext,
  Item,
  Json,
  NodeResult,
  Workflow,
  WFNode,
} from '@crosscraft/schema';
import { Registry } from './registry';
import { resolveParams, type ExprScope } from './expression';
import {
  createExecution,
  finishExecution,
  finishStep,
  getCredentialData,
  loadExecution,
  saveState,
  setWaiting,
  startStep,
  type RunState,
} from './store';

export interface RunResult {
  executionId: string;
  status: 'success' | 'error' | 'waiting';
  /** Optional response a suspending node asked us to return to the caller. */
  respond?: { status?: number; body?: Json };
  /** Outputs of terminal nodes (handy for an immediate response on success). */
  outputs?: Record<string, Item[]>;
  error?: string;
}

function incomingEdges(wf: Workflow, nodeId: string) {
  return wf.edges.filter((e) => e.target === nodeId);
}
function outgoingEdges(wf: Workflow, nodeId: string) {
  return wf.edges.filter((e) => e.source === nodeId);
}

/** Gather a node's input items from its upstream sources (or the trigger payload). */
function gatherInput(wf: Workflow, nodeId: string, state: RunState): Item[] {
  const incoming = incomingEdges(wf, nodeId);
  if (incoming.length === 0) return state.triggerItems;
  const items: Item[] = [];
  for (const e of incoming) {
    const port = e.sourceHandle ?? DEFAULT_PORT;
    const out = state.nodeOutputs[e.source]?.[port] ?? [];
    items.push(...out);
  }
  return items;
}

function isReady(wf: Workflow, nodeId: string, visited: Set<string>): boolean {
  return incomingEdges(wf, nodeId).every((e) => visited.has(e.source));
}

function enqueueReadySuccessors(
  wf: Workflow,
  nodeId: string,
  visited: Set<string>,
  ready: string[],
) {
  for (const e of outgoingEdges(wf, nodeId)) {
    if (!visited.has(e.target) && !ready.includes(e.target) && isReady(wf, e.target, visited)) {
      ready.push(e.target);
    }
  }
}

function buildContext(
  wf: Workflow,
  node: WFNode,
  executionId: string,
  state: RunState,
  inputItems: Item[],
  logs: { message: string; data?: Json }[],
): ExecContext {
  const scope: ExprScope = {
    $json: inputItems[0]?.json ?? {},
    $input: inputItems,
    $trigger: state.triggerItems,
    $node: (id: string) => state.nodeOutputs[id]?.[DEFAULT_PORT] ?? [],
  };
  const params = resolveParams(node.params, scope);
  return {
    input: inputItems,
    params,
    rawParam: (name) => node.params[name],
    upstream: (id) => state.nodeOutputs[id]?.[DEFAULT_PORT] ?? [],
    credential: async (paramName) => {
      const id = params[paramName];
      if (typeof id !== 'string' || !id) return null;
      return getCredentialData(id);
    },
    trigger: state.triggerItems,
    log: (message, data) => logs.push({ message, data }),
    first: () => inputItems[0]?.json ?? {},
    ids: { workflowId: wf.id, executionId, nodeId: node.id },
  };
}

/** Core loop shared by run() and resume(). Mutates `state`. */
async function drive(
  wf: Workflow,
  registry: Registry,
  executionId: string,
  state: RunState,
  ready: string[],
): Promise<RunResult> {
  const visited = new Set(state.visited);

  while (ready.length > 0) {
    const nodeId = ready.shift()!;
    if (visited.has(nodeId)) continue;
    const node = wf.nodes.find((n) => n.id === nodeId);
    if (!node) continue;

    const def = registry.get(node.type);
    const inputItems = gatherInput(wf, nodeId, state);

    // Prune untaken branches: a non-trigger node with connected inputs but no items is skipped.
    if (!def.isTrigger && incomingEdges(wf, nodeId).length > 0 && inputItems.length === 0) {
      state.nodeOutputs[nodeId] = { [DEFAULT_PORT]: [] };
      visited.add(nodeId);
      state.visited = [...visited];
      enqueueReadySuccessors(wf, nodeId, visited, ready);
      continue;
    }

    const logs: { message: string; data?: Json }[] = [];
    const stepId = await startStep(executionId, nodeId, inputItems);
    let result: NodeResult;
    try {
      const ctx = buildContext(wf, node, executionId, state, inputItems, logs);
      result = await def.execute(ctx);
    } catch (e) {
      const error = (e as Error).message;
      await finishStep(stepId, 'error', [], logs, error);
      await finishExecution(executionId, 'error');
      return { executionId, status: 'error', error };
    }

    if ('suspend' in result) {
      // Mark the step as completed-pending and persist state for resume.
      await finishStep(stepId, 'success', [], [...logs, { message: 'suspended; awaiting resume' }]);
      state.visited = [...visited]; // waiting node intentionally NOT yet visited
      const resumeToken = nanoid();
      await setWaiting(executionId, nodeId, resumeToken, state);
      return { executionId, status: 'waiting', respond: result.suspend.respond };
    }

    state.nodeOutputs[nodeId] = result.outputs;
    visited.add(nodeId);
    state.visited = [...visited];
    const mainOut = result.outputs[DEFAULT_PORT] ?? [];
    await finishStep(stepId, 'success', flattenOutputs(result.outputs), logs);
    await saveState(executionId, state);
    void mainOut;
    enqueueReadySuccessors(wf, nodeId, visited, ready);
  }

  await finishExecution(executionId, 'success');
  return { executionId, status: 'success', outputs: terminalOutputs(wf, state) };
}

function flattenOutputs(outputs: Record<string, Item[]>): Item[] {
  return Object.values(outputs).flat();
}

function terminalOutputs(wf: Workflow, state: RunState): Record<string, Item[]> {
  const out: Record<string, Item[]> = {};
  for (const n of wf.nodes) {
    const produced = state.nodeOutputs[n.id];
    if (outgoingEdges(wf, n.id).length === 0 && produced) {
      out[n.id] = produced[DEFAULT_PORT] ?? [];
    }
  }
  return out;
}

function findTrigger(wf: Workflow, registry: Registry): WFNode {
  const explicit = wf.nodes.find((n) => registry.has(n.type) && registry.get(n.type).isTrigger);
  if (explicit) return explicit;
  const roots = wf.nodes.filter((n) => incomingEdges(wf, n.id).length === 0);
  if (roots.length === 0) throw new Error('Workflow has no trigger / root node');
  return roots[0]!;
}

/** Start a new execution of a workflow. `triggerItems` is the trigger/webhook payload. */
export async function run(
  wf: Workflow,
  registry: Registry,
  opts: { triggerItems?: Item[] } = {},
): Promise<RunResult> {
  const trigger = findTrigger(wf, registry);
  const executionId = await createExecution(wf.id);
  const state: RunState = {
    triggerItems: opts.triggerItems ?? [{ json: {} }],
    nodeOutputs: {},
    visited: [],
  };
  return drive(wf, registry, executionId, state, [trigger.id]);
}

/** Resume a waiting execution with the payload that arrived (the resumed node's output). */
export async function resume(
  executionId: string,
  registry: Registry,
  resumePayload: Item[],
  loadWorkflow: (id: string) => Promise<Workflow | null>,
): Promise<RunResult> {
  const exec = await loadExecution(executionId);
  if (!exec) throw new Error(`Execution not found: ${executionId}`);
  if (exec.status !== 'waiting' || !exec.state || !exec.waitingNodeId) {
    throw new Error(`Execution ${executionId} is not waiting (status=${exec.status})`);
  }
  const wf = await loadWorkflow(exec.workflowId);
  if (!wf) throw new Error(`Workflow not found: ${exec.workflowId}`);

  const state = exec.state;
  const waitingNodeId = exec.waitingNodeId;
  // The resumed node "produces" the incoming payload as its output, then we continue.
  state.nodeOutputs[waitingNodeId] = { [DEFAULT_PORT]: resumePayload };
  const visited = new Set(state.visited);
  visited.add(waitingNodeId);
  state.visited = [...visited];

  const ready: string[] = [];
  enqueueReadySuccessors(wf, waitingNodeId, visited, ready);
  return drive(wf, registry, executionId, state, ready);
}
