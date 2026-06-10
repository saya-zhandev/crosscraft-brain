import { NextResponse } from 'next/server';
import { nanoid } from 'nanoid';
import { structured, MODELS } from '@crosscraft/nodes-ai';
import type { GraphOp, Workflow } from '@crosscraft/schema';
import { registry } from '@/lib/registry';

// Natural-language workflow copilot: given the node catalog + current graph, ask Claude
// for the nodes/edges to add, then translate them into GraphOps the canvas applies.
export async function POST(req: Request) {
  const { message, workflow } = (await req.json()) as { message: string; workflow: Workflow };

  const catalog = registry()
    .descriptors()
    .map((d) => ({
      type: d.type,
      label: d.label,
      group: d.group,
      outputs: d.outputs.map((o) => o.id),
      params: d.params.map((p) => p.name),
    }));

  const system = [
    'You build node-based automation workflows for the "crosscraft" engine.',
    'Only use node `type` values from the provided catalog. Wire nodes with edges.',
    'Triggers have no inputs and start the flow. The "if" node has outputs "true" and "false".',
    'Return the nodes and edges to ADD to the current graph. Reuse existing node ids when connecting to them.',
    'Give each new node a short unique id and sensible params (param values may use {{ $json.field }} expressions).',
  ].join(' ');

  const prompt = JSON.stringify({ request: message, catalog, currentGraph: { nodes: workflow.nodes, edges: workflow.edges } });

  try {
    const result = await structured<{
      message: string;
      nodes: { id: string; type: string; params?: Record<string, unknown>; x?: number; y?: number }[];
      edges: { source: string; target: string; sourceHandle?: string; targetHandle?: string }[];
    }>({
      model: MODELS.smart,
      system,
      prompt,
      toolName: 'build_workflow',
      schema: {
        type: 'object',
        properties: {
          message: { type: 'string', description: 'Short explanation for the user.' },
          nodes: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                id: { type: 'string' },
                type: { type: 'string' },
                params: { type: 'object', additionalProperties: true },
                x: { type: 'number' },
                y: { type: 'number' },
              },
              required: ['id', 'type'],
            },
          },
          edges: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                source: { type: 'string' },
                target: { type: 'string' },
                sourceHandle: { type: 'string' },
                targetHandle: { type: 'string' },
              },
              required: ['source', 'target'],
            },
          },
        },
        required: ['message', 'nodes', 'edges'],
      },
    });

    const ops: GraphOp[] = [];
    result.nodes.forEach((n, i) => {
      if (!registry().has(n.type)) return; // ignore hallucinated node types
      ops.push({
        op: 'addNode',
        node: { id: n.id, type: n.type, params: n.params ?? {}, position: { x: n.x ?? i * 240, y: n.y ?? 80 } },
      });
    });
    for (const e of result.edges) {
      ops.push({
        op: 'connect',
        edge: { id: nanoid(), source: e.source, target: e.target, sourceHandle: e.sourceHandle ?? 'main', targetHandle: e.targetHandle ?? 'main' },
      });
    }

    return NextResponse.json({ ops, message: result.message });
  } catch (e) {
    return NextResponse.json({ error: (e as Error).message }, { status: 200 });
  }
}
