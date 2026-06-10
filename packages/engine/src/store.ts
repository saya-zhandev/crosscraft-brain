/** Persistence for executions, steps, workflows and credentials. */
import { nanoid } from 'nanoid';
import type {
  ExecutionRecord,
  ExecutionStatus,
  Item,
  Json,
  StepRecord,
  StepStatus,
  Workflow,
} from '@crosscraft/schema';
import { query } from './db';
import { decrypt } from './crypto';

/** Serialized run state stashed on the execution row so a waiting run can resume. */
export interface RunState {
  triggerItems: Item[];
  nodeOutputs: Record<string, Record<string, Item[]>>;
  visited: string[];
}

// ---- workflows ----
export async function saveWorkflow(wf: Workflow): Promise<Workflow> {
  await query(
    `INSERT INTO workflows (id, name, graph, active, version, updated_at)
     VALUES ($1,$2,$3,$4,1, now())
     ON CONFLICT (id) DO UPDATE SET name=$2, graph=$3, active=$4,
       version=workflows.version+1, updated_at=now()`,
    [wf.id, wf.name, JSON.stringify(wf), wf.active],
  );
  return wf;
}

export async function getWorkflow(id: string): Promise<Workflow | null> {
  const r = await query<{ graph: Workflow }>(`SELECT graph FROM workflows WHERE id=$1`, [id]);
  return r.rows[0]?.graph ?? null;
}

export async function listWorkflows(): Promise<Pick<Workflow, 'id' | 'name' | 'active'>[]> {
  const r = await query<{ id: string; name: string; active: boolean }>(
    `SELECT id, name, active FROM workflows ORDER BY updated_at DESC`,
  );
  return r.rows;
}

// ---- executions ----
export async function createExecution(workflowId: string): Promise<string> {
  const id = nanoid();
  await query(
    `INSERT INTO executions (id, workflow_id, status, started_at) VALUES ($1,$2,'running',now())`,
    [id, workflowId],
  );
  return id;
}

export async function setWaiting(
  id: string,
  waitingNodeId: string,
  resumeToken: string,
  state: RunState,
): Promise<void> {
  await query(
    `UPDATE executions SET status='waiting', waiting_node_id=$2, resume_token=$3, state=$4 WHERE id=$1`,
    [id, waitingNodeId, resumeToken, JSON.stringify(state)],
  );
}

export async function saveState(id: string, state: RunState): Promise<void> {
  await query(`UPDATE executions SET state=$2 WHERE id=$1`, [id, JSON.stringify(state)]);
}

export async function finishExecution(id: string, status: ExecutionStatus): Promise<void> {
  await query(
    `UPDATE executions SET status=$2, finished_at=now(), waiting_node_id=NULL, resume_token=NULL WHERE id=$1`,
    [id, status],
  );
}

export async function loadExecution(
  id: string,
): Promise<(ExecutionRecord & { state: RunState | null }) | null> {
  const r = await query<{
    id: string;
    workflow_id: string;
    status: ExecutionStatus;
    resume_token: string | null;
    waiting_node_id: string | null;
    started_at: string;
    finished_at: string | null;
    state: RunState | null;
  }>(`SELECT * FROM executions WHERE id=$1`, [id]);
  const row = r.rows[0];
  if (!row) return null;
  return {
    id: row.id,
    workflowId: row.workflow_id,
    status: row.status,
    resumeToken: row.resume_token,
    waitingNodeId: row.waiting_node_id,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    state: row.state,
  };
}

export async function listExecutions(workflowId?: string): Promise<ExecutionRecord[]> {
  const r = workflowId
    ? await query(`SELECT * FROM executions WHERE workflow_id=$1 ORDER BY started_at DESC LIMIT 100`, [workflowId])
    : await query(`SELECT * FROM executions ORDER BY started_at DESC LIMIT 100`);
  return r.rows.map((row: Record<string, unknown>) => ({
    id: row.id as string,
    workflowId: row.workflow_id as string,
    status: row.status as ExecutionStatus,
    resumeToken: row.resume_token as string | null,
    waitingNodeId: row.waiting_node_id as string | null,
    startedAt: row.started_at as string,
    finishedAt: row.finished_at as string | null,
  }));
}

// ---- steps (monitoring) ----
export async function startStep(executionId: string, nodeId: string, input: Item[]): Promise<string> {
  const id = nanoid();
  await query(
    `INSERT INTO execution_steps (id, execution_id, node_id, status, input, started_at)
     VALUES ($1,$2,$3,'running',$4, now())`,
    [id, executionId, nodeId, JSON.stringify(input)],
  );
  return id;
}

export async function finishStep(
  stepId: string,
  status: StepStatus,
  output: Item[],
  logs: { message: string; data?: Json }[],
  error?: string | null,
): Promise<void> {
  await query(
    `UPDATE execution_steps SET status=$2, output=$3, logs=$4, error=$5, finished_at=now() WHERE id=$1`,
    [stepId, status, JSON.stringify(output), JSON.stringify(logs), error ?? null],
  );
}

export async function getExecutionSteps(executionId: string): Promise<StepRecord[]> {
  const r = await query(
    `SELECT * FROM execution_steps WHERE execution_id=$1 ORDER BY started_at ASC`,
    [executionId],
  );
  return r.rows.map((row: Record<string, unknown>) => ({
    id: row.id as string,
    executionId: row.execution_id as string,
    nodeId: row.node_id as string,
    status: row.status as StepStatus,
    input: (row.input as Item[]) ?? [],
    output: (row.output as Item[]) ?? [],
    logs: (row.logs as { message: string; data?: Json }[]) ?? [],
    error: row.error as string | null,
    startedAt: row.started_at as string,
    finishedAt: row.finished_at as string | null,
  }));
}

// ---- credentials ----
export async function getCredentialData(id: string): Promise<Record<string, unknown> | null> {
  const r = await query<{ data_encrypted: string }>(
    `SELECT data_encrypted FROM credentials WHERE id=$1`,
    [id],
  );
  const row = r.rows[0];
  if (!row) return null;
  return JSON.parse(decrypt(row.data_encrypted)) as Record<string, unknown>;
}
