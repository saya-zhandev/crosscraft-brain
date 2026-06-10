/**
 * Core nodes shipped with the skeleton. These are vertical-agnostic building blocks;
 * a fork registers additional NodeDefinitions alongside these.
 */
import type { Item, Json, NodeDefinition } from '@crosscraft/schema';
import { resolveValue, type ExprScope } from '@crosscraft/engine';

const itemsOrEmpty = (input: Item[]): Item[] => (input.length ? input : [{ json: {} }]);

function scopeFor(item: Item, ctx: { input: Item[]; trigger: Item[]; upstream: (id: string) => Item[] }): ExprScope {
  return { $json: item.json, $input: ctx.input, $trigger: ctx.trigger, $node: ctx.upstream };
}

// --- Trigger: Manual ---------------------------------------------------------
const manualTrigger: NodeDefinition = {
  type: 'core.manualTrigger',
  label: 'Manual Trigger',
  group: 'trigger',
  icon: 'Play',
  description: 'Starts the workflow when you click Run.',
  isTrigger: true,
  inputs: [],
  outputs: [{ id: 'main' }],
  params: [],
  async execute(ctx) {
    return { outputs: { main: ctx.trigger.length ? ctx.trigger : [{ json: {} }] } };
  },
};

// --- Trigger: Webhook --------------------------------------------------------
const webhookTrigger: NodeDefinition = {
  type: 'core.webhookTrigger',
  label: 'Webhook',
  group: 'trigger',
  icon: 'Webhook',
  description: 'Starts the workflow when POSTed to /api/webhook/{path}. Body becomes the item.',
  isTrigger: true,
  inputs: [],
  outputs: [{ id: 'main' }],
  params: [
    { name: 'path', label: 'Path', type: 'string', required: true, placeholder: 'my-hook' },
  ],
  async execute(ctx) {
    return { outputs: { main: ctx.trigger.length ? ctx.trigger : [{ json: {} }] } };
  },
};

// --- Transform: Set ----------------------------------------------------------
const set: NodeDefinition = {
  type: 'core.set',
  label: 'Set Fields',
  group: 'transform',
  icon: 'PencilLine',
  description: 'Merge a set of (expression-aware) fields onto each item.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    {
      name: 'fields',
      label: 'Fields (JSON object; values may use {{ }})',
      type: 'json',
      default: {},
    },
  ],
  async execute(ctx) {
    const out = itemsOrEmpty(ctx.input).map((item) => {
      const fields = ctx.rawParam('fields');
      const obj = typeof fields === 'string' ? safeParse(fields) : (fields as Record<string, unknown>) ?? {};
      const resolved: Record<string, Json> = {};
      const scope = scopeFor(item, ctx);
      for (const [k, v] of Object.entries(obj)) resolved[k] = resolveValue(v, scope) as Json;
      return { json: { ...item.json, ...resolved } };
    });
    return { outputs: { main: out } };
  },
};

// --- Flow: If ----------------------------------------------------------------
const ifNode: NodeDefinition = {
  type: 'core.if',
  label: 'If',
  group: 'flow',
  icon: 'GitBranch',
  description: 'Route each item to true/false based on a condition expression.',
  inputs: [{ id: 'main' }],
  outputs: [
    { id: 'true', label: 'true' },
    { id: 'false', label: 'false' },
  ],
  params: [
    {
      name: 'condition',
      label: 'Condition ({{ }} expression, must be truthy/falsy)',
      type: 'expression',
      required: true,
      placeholder: '{{ $json.amount > 100 }}',
    },
  ],
  async execute(ctx) {
    const t: Item[] = [];
    const f: Item[] = [];
    const raw = ctx.rawParam('condition');
    for (const item of itemsOrEmpty(ctx.input)) {
      const val = resolveValue(raw, scopeFor(item, ctx));
      (val ? t : f).push(item);
    }
    return { outputs: { true: t, false: f } };
  },
};

// --- Integration: HTTP Request ----------------------------------------------
const http: NodeDefinition = {
  type: 'core.http',
  label: 'HTTP Request',
  group: 'integration',
  icon: 'Globe',
  description: 'Call an external HTTP API.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    {
      name: 'method',
      label: 'Method',
      type: 'select',
      default: 'GET',
      options: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((m) => ({ label: m, value: m })),
    },
    { name: 'url', label: 'URL', type: 'expression', required: true, placeholder: 'https://api.example.com' },
    { name: 'headers', label: 'Headers (JSON)', type: 'json', default: {} },
    { name: 'body', label: 'Body (JSON)', type: 'json', default: {} },
  ],
  async execute(ctx) {
    const method = String(ctx.params.method ?? 'GET');
    const url = String(ctx.params.url ?? '');
    if (!url) throw new Error('HTTP node: URL is required');
    const headers = asObject(ctx.params.headers);
    const init: RequestInit = { method, headers: { ...headers } as Record<string, string> };
    if (method !== 'GET' && method !== 'DELETE') {
      const body = asObject(ctx.params.body);
      (init.headers as Record<string, string>)['content-type'] ??= 'application/json';
      init.body = JSON.stringify(body);
    }
    ctx.log(`${method} ${url}`);
    const res = await fetch(url, init);
    const text = await res.text();
    let data: Json;
    try {
      data = JSON.parse(text) as Json;
    } catch {
      data = text;
    }
    return { outputs: { main: [{ json: { status: res.status, ok: res.ok, body: data } } as Item] } };
  },
};

// --- Flow: Code --------------------------------------------------------------
const code: NodeDefinition = {
  type: 'core.code',
  label: 'Code',
  group: 'flow',
  icon: 'Code',
  description: 'Run JavaScript. Receives `items` (array), must return an array of items.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [
    {
      name: 'code',
      label: 'JavaScript',
      type: 'expression',
      default: 'return items;',
      description: 'Vars: items, $trigger. Return Item[] or plain objects.',
    },
  ],
  async execute(ctx) {
    const src = String(ctx.rawParam('code') ?? 'return items;');
    const fn = new Function('items', '$trigger', `"use strict"; ${src}`);
    const result = fn(itemsOrEmpty(ctx.input), ctx.trigger);
    const arr = Array.isArray(result) ? result : [result];
    const out = arr.map((r) => (r && typeof r === 'object' && 'json' in r ? (r as Item) : { json: r as Record<string, Json> }));
    return { outputs: { main: out } };
  },
};

// --- Flow: Wait (durable suspend/resume) ------------------------------------
const wait: NodeDefinition = {
  type: 'core.wait',
  label: 'Wait for Webhook',
  group: 'flow',
  icon: 'Clock',
  description: 'Pause the run until POST /api/resume/{executionId} is called. The next item is the resume payload.',
  inputs: [{ id: 'main' }],
  outputs: [{ id: 'main' }],
  params: [],
  async execute(ctx) {
    return {
      suspend: {
        kind: 'webhook',
        respond: { body: { executionId: ctx.ids.executionId, status: 'waiting' } },
      },
    };
  },
};

function safeParse(s: string): Record<string, unknown> {
  try {
    return JSON.parse(s) as Record<string, unknown>;
  } catch {
    return {};
  }
}
function asObject(v: unknown): Record<string, unknown> {
  if (typeof v === 'string') return safeParse(v);
  if (v && typeof v === 'object') return v as Record<string, unknown>;
  return {};
}

export const coreNodes: NodeDefinition[] = [
  manualTrigger,
  webhookTrigger,
  set,
  ifNode,
  http,
  code,
  wait,
];
