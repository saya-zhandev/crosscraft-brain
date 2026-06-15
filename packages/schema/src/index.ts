/**
 * @crosscraft/schema — the contract shared by every package.
 *
 * The canvas, engine, registry, copilot and SDK all speak these types. Nothing here
 * is vertical-specific: a fork adds NodeDefinitions, it never edits this file.
 */

// ---------------------------------------------------------------------------
// Workflow graph (what the canvas saves and the engine runs)
// ---------------------------------------------------------------------------

/** A single arbitrary JSON value flowing between nodes. */
export type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

/** A data item = one JSON object on a connection. Nodes emit/consume arrays of these. */
export type Item = { json: Record<string, Json>; binary?: Record<string, BinaryRef> };

/** Reference to binary data (kept out of the JSON item; e.g. an uploaded photo or a PDF). */
export type BinaryRef = { data: string; mimeType: string; fileName?: string };

export interface WFNode {
  id: string;
  type: string; // matches a NodeDefinition.type in the registry
  params: Record<string, unknown>;
  position: { x: number; y: number };
  name?: string; // optional human label override shown on the canvas
}

export interface WFEdge {
  id: string;
  source: string; // WFNode.id
  sourceHandle?: string; // output port id (default "main")
  target: string; // WFNode.id
  targetHandle?: string; // input port id (default "main")
}

export interface Workflow {
  id: string;
  name: string;
  active: boolean;
  nodes: WFNode[];
  edges: WFEdge[];
  settings?: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Node authoring contract (how a node/integration declares itself)
// ---------------------------------------------------------------------------

export type NodeGroup = 'trigger' | 'transform' | 'flow' | 'integration' | 'ai';

export interface Port {
  id: string; // "main", "true", "false", ...
  label?: string;
}

export type ParamType =
  | 'string'
  | 'number'
  | 'boolean'
  | 'select'
  | 'json'
  | 'expression' // a string that may contain {{ ... }} expressions
  | 'credential';

export interface ParamSchema {
  name: string;
  label: string;
  type: ParamType;
  required?: boolean;
  default?: unknown;
  placeholder?: string;
  description?: string;
  options?: { label: string; value: string }[]; // for type: 'select'
  credentialType?: string; // for type: 'credential'
  /** Only show this param when another param has one of these values. */
  showWhen?: { param: string; equals: unknown[] };
}

/** What a node returns: items per output port, OR a suspend request (durable wait). */
export type NodeResult =
  | { outputs: Record<string, Item[]> } // keyed by output port id
  | { suspend: SuspendRequest };

export interface SuspendRequest {
  kind: 'webhook'; // MVP: resume via external webhook/resume call
  /** Optional response returned to the caller that triggered execution while we suspend. */
  respond?: { status?: number; body?: Json };
}

/** Runtime context handed to a node's execute(). Concrete impl lives in @crosscraft/engine. */
export interface ExecContext {
  /** Items arriving on the node's primary input. */
  input: Item[];
  /** Resolved param values (expressions already evaluated against the run context). */
  params: Record<string, unknown>;
  /** Raw (un-evaluated) param value, for nodes that want the template string. */
  rawParam(name: string): unknown;
  /** Output items of any already-executed upstream node, by node id. */
  upstream(nodeId: string): Item[];
  /** Decrypted credential data for a credential param, if set. */
  credential(paramName: string): Promise<Record<string, unknown> | null>;
  /** Trigger/resume payload that entered the run (webhook body, etc.). */
  trigger: Item[];
  /** Structured logging captured into the step record. */
  log(message: string, data?: Json): void;
  /** Helper for nodes that need a single merged JSON object from the first input item. */
  first(): Record<string, Json>;
  ids: { workflowId: string; executionId: string; nodeId: string };
}

export interface NodeDefinition {
  type: string;
  label: string;
  group: NodeGroup;
  icon?: string; // lucide-react icon name
  description?: string;
  inputs: Port[];
  outputs: Port[];
  params: ParamSchema[];
  credentials?: string[];
  /** A trigger node starts a run; non-trigger nodes are executed when reached. */
  isTrigger?: boolean;
  execute(ctx: ExecContext): Promise<NodeResult>;
}

/** Serializable node metadata (a NodeDefinition without execute) — sent to the canvas. */
export type NodeDescriptor = Omit<NodeDefinition, 'execute'>;

// ---------------------------------------------------------------------------
// Execution / monitoring records (persisted; power Transparent Monitoring)
// ---------------------------------------------------------------------------

export type ExecutionStatus = 'running' | 'waiting' | 'success' | 'error';
export type StepStatus = 'running' | 'success' | 'error';

export interface ExecutionRecord {
  id: string;
  workflowId: string;
  status: ExecutionStatus;
  resumeToken?: string | null;
  waitingNodeId?: string | null;
  startedAt: string;
  finishedAt?: string | null;
}

export interface StepRecord {
  id: string;
  executionId: string;
  nodeId: string;
  status: StepStatus;
  input: Item[];
  output: Item[];
  logs?: { message: string; data?: Json }[];
  error?: string | null;
  startedAt: string;
  finishedAt?: string | null;
}

// ---------------------------------------------------------------------------
// Copilot graph operations (AI emits these; canvas applies them)
// ---------------------------------------------------------------------------

export type GraphOp =
  | { op: 'addNode'; node: WFNode }
  | { op: 'connect'; edge: WFEdge }
  | { op: 'setParam'; nodeId: string; param: string; value: unknown }
  | { op: 'removeNode'; nodeId: string };

export const DEFAULT_PORT = 'main';
