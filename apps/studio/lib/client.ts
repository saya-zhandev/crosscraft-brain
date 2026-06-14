'use client';
import type { Workflow } from '@crosscraft/schema';
import type { NodeDescriptor } from '@crosscraft/engine';

const j = async <T>(r: Response): Promise<T> => {
  if (!r.ok) throw new Error((await r.text()) || r.statusText);
  return r.json() as Promise<T>;
};

export const api = {
  nodes: () => fetch('/api/nodes').then((r) => j<NodeDescriptor[]>(r)),
  listWorkflows: () => fetch('/api/workflows').then((r) => j<Pick<Workflow, 'id' | 'name' | 'active'>[]>(r)),
  createWorkflow: (name: string) =>
    fetch('/api/workflows', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name }),
    }).then((r) => j<Workflow>(r)),
  getWorkflow: (id: string) => fetch(`/api/workflows/${id}`).then((r) => j<Workflow>(r)),
  saveWorkflow: (wf: Workflow) =>
    fetch(`/api/workflows/${wf.id}`, {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(wf),
    }).then((r) => j<Workflow>(r)),
  run: (id: string, body: unknown = {}) =>
    fetch(`/api/workflows/${id}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    }).then((r) => j<{ executionId: string; status: string }>(r)),
  listExecutions: (workflowId: string) =>
    fetch(`/api/executions?workflowId=${workflowId}`).then((r) => j<ExecutionRow[]>(r)),
  resume: (executionId: string, body: unknown = {}) =>
    fetch(`/api/resume/${executionId}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    }).then((r) => j<{ executionId: string; status?: string }>(r)),
  listCredentials: () => fetch('/api/credentials').then((r) => j<CredentialRow[]>(r)),
  createCredential: (input: { type: string; name: string; data: Record<string, unknown> }) =>
    fetch('/api/credentials', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(input),
    }).then((r) => j<CredentialRow>(r)),
  deleteCredential: (id: string) =>
    fetch(`/api/credentials/${id}`, { method: 'DELETE' }).then((r) => j<{ ok: true }>(r)),
};

export interface CredentialRow {
  id: string;
  type: string;
  name: string;
}

export interface ExecutionRow {
  id: string;
  workflowId: string;
  status: 'running' | 'waiting' | 'success' | 'error';
  startedAt: string;
  finishedAt: string | null;
}
