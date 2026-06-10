'use client';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Node,
  type Edge,
  type Connection,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { nanoid } from 'nanoid';
import Link from 'next/link';
import type { NodeDescriptor } from '@crosscraft/engine';
import type { GraphOp, StepRecord, Workflow } from '@crosscraft/schema';
import { api } from '@/lib/client';
import { CcNode, type CcNodeData } from './CcNode';
import { Inspector } from './Inspector';
import { Palette } from './Palette';
import { Copilot } from './Copilot';

const nodeTypes: NodeTypes = { cc: CcNode };

function makeFlowNode(
  desc: NodeDescriptor,
  opts: { id?: string; name?: string; params?: Record<string, unknown>; position: { x: number; y: number } },
): Node {
  const params: Record<string, unknown> = { ...opts.params };
  for (const p of desc.params) if (params[p.name] === undefined && p.default !== undefined) params[p.name] = p.default;
  return {
    id: opts.id ?? nanoid(),
    type: 'cc',
    position: opts.position,
    data: { label: opts.name ?? desc.label, descriptor: desc, params } satisfies CcNodeData,
  };
}

function EditorInner({ workflow }: { workflow: Workflow }) {
  const [descriptors, setDescriptors] = useState<NodeDescriptor[]>([]);
  const byType = useMemo(() => new Map(descriptors.map((d) => [d.type, d])), [descriptors]);
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [name, setName] = useState(workflow.name);
  const [active, setActive] = useState(workflow.active);
  const [status, setStatus] = useState<string>('');
  const [steps, setSteps] = useState<Record<string, StepRecord>>({});
  const [copilotOpen, setCopilotOpen] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition } = useReactFlow();

  // Load node catalog, then hydrate the graph (needs descriptors to render nodes).
  useEffect(() => {
    api.nodes().then((descs) => {
      setDescriptors(descs);
      const map = new Map(descs.map((d) => [d.type, d]));
      setNodes(
        workflow.nodes
          .filter((n) => map.has(n.type))
          .map((n) => makeFlowNode(map.get(n.type)!, { id: n.id, name: n.name, params: n.params, position: n.position })),
      );
      setEdges(
        workflow.edges.map((e) => ({
          id: e.id,
          source: e.source,
          target: e.target,
          sourceHandle: e.sourceHandle ?? 'main',
          targetHandle: e.targetHandle ?? 'main',
        })),
      );
    });
  }, [workflow, setNodes, setEdges]);

  const onConnect = useCallback(
    (c: Connection) => setEdges((eds) => addEdge({ ...c, id: nanoid() }, eds)),
    [setEdges],
  );

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const type = e.dataTransfer.getData('application/cc-node');
      const desc = byType.get(type);
      if (!desc) return;
      const position = screenToFlowPosition({ x: e.clientX, y: e.clientY });
      setNodes((nds) => [...nds, makeFlowNode(desc, { position })]);
    },
    [byType, screenToFlowPosition, setNodes],
  );

  const toWorkflow = useCallback(
    (): Workflow => ({
      id: workflow.id,
      name,
      active,
      nodes: nodes.map((n) => {
        const d = n.data as CcNodeData;
        return { id: n.id, type: d.descriptor.type, name: d.label, params: d.params, position: n.position };
      }),
      edges: edges.map((e) => ({
        id: e.id,
        source: e.source,
        sourceHandle: e.sourceHandle ?? 'main',
        target: e.target,
        targetHandle: e.targetHandle ?? 'main',
      })),
    }),
    [workflow.id, name, active, nodes, edges],
  );

  const save = useCallback(async () => {
    setStatus('saving…');
    await api.saveWorkflow(toWorkflow());
    setStatus('saved');
  }, [toWorkflow]);

  const run = useCallback(async () => {
    await api.saveWorkflow(toWorkflow());
    setSteps({});
    setStatus('running…');
    const { executionId } = await api.run(workflow.id, {});
    const es = new EventSource(`/api/executions/${executionId}/stream`);
    es.onmessage = (ev) => {
      const data = JSON.parse(ev.data) as { status: string; waitingNodeId?: string; steps: StepRecord[] };
      const map: Record<string, StepRecord> = {};
      for (const s of data.steps) map[s.nodeId] = s;
      setSteps(map);
      setStatus(
        data.status === 'waiting'
          ? `waiting · resume: POST /api/resume/${executionId}`
          : data.status,
      );
      setNodes((nds) =>
        nds.map((n) => {
          const st = map[n.id]?.status;
          const isWaiting = data.status === 'waiting' && data.waitingNodeId === n.id;
          return { ...n, data: { ...n.data, status: isWaiting ? 'waiting' : st } };
        }),
      );
      if (data.status === 'success' || data.status === 'error') es.close();
    };
    es.onerror = () => es.close();
  }, [toWorkflow, workflow.id, setNodes]);

  const applyOps = useCallback(
    (ops: GraphOp[]) => {
      for (const op of ops) {
        if (op.op === 'addNode') {
          const desc = byType.get(op.node.type);
          if (!desc) continue;
          setNodes((nds) => [
            ...nds,
            makeFlowNode(desc, { id: op.node.id, name: op.node.name, params: op.node.params, position: op.node.position }),
          ]);
        } else if (op.op === 'connect') {
          setEdges((eds) =>
            addEdge(
              { id: op.edge.id || nanoid(), source: op.edge.source, target: op.edge.target, sourceHandle: op.edge.sourceHandle ?? 'main', targetHandle: op.edge.targetHandle ?? 'main' },
              eds,
            ),
          );
        } else if (op.op === 'setParam') {
          setNodes((nds) =>
            nds.map((n) =>
              n.id === op.nodeId ? { ...n, data: { ...n.data, params: { ...(n.data as CcNodeData).params, [op.param]: op.value } } } : n,
            ),
          );
        } else if (op.op === 'removeNode') {
          setNodes((nds) => nds.filter((n) => n.id !== op.nodeId));
        }
      }
    },
    [byType, setNodes, setEdges],
  );

  const selNode = nodes.find((n) => n.id === selected);
  const selDesc = selNode ? (selNode.data as CcNodeData).descriptor : null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      {/* Top bar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', borderBottom: '1px solid var(--border)' }}>
        <Link href="/" className="btn" style={{ padding: '6px 10px' }}>
          ←
        </Link>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          style={{ background: 'transparent', border: 'none', color: 'var(--text)', fontSize: 15, fontWeight: 600, outline: 'none' }}
        />
        <div style={{ flex: 1 }} />
        <span style={{ fontSize: 12, color: 'var(--muted)' }}>{status}</span>
        <label style={{ fontSize: 12, color: 'var(--muted)', display: 'flex', gap: 5, alignItems: 'center' }}>
          <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
          active
        </label>
        <button className="btn" onClick={() => setCopilotOpen((v) => !v)}>
          ✨ Copilot
        </button>
        <Link href={`/executions/${workflow.id}`} className="btn">
          Runs
        </Link>
        <button className="btn" onClick={save}>
          Save
        </button>
        <button className="btn btn-accent" onClick={run}>
          ▶ Run
        </button>
      </div>

      {/* Body */}
      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        <Palette descriptors={descriptors} />
        <div ref={wrapper} style={{ flex: 1 }} onDrop={onDrop} onDragOver={(e) => e.preventDefault()}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_, n) => setSelected(n.id)}
            onPaneClick={() => setSelected(null)}
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background color="#1c2230" gap={18} />
            <Controls />
          </ReactFlow>
        </div>
        {selNode && selDesc ? (
          <Inspector
            descriptor={selDesc}
            name={(selNode.data as CcNodeData).label}
            params={(selNode.data as CcNodeData).params}
            step={steps[selNode.id]}
            onRename={(nm) =>
              setNodes((nds) => nds.map((n) => (n.id === selNode.id ? { ...n, data: { ...n.data, label: nm } } : n)))
            }
            onChange={(params) =>
              setNodes((nds) => nds.map((n) => (n.id === selNode.id ? { ...n, data: { ...n.data, params } } : n)))
            }
            onDelete={() => {
              setNodes((nds) => nds.filter((n) => n.id !== selNode.id));
              setEdges((eds) => eds.filter((e) => e.source !== selNode.id && e.target !== selNode.id));
              setSelected(null);
            }}
          />
        ) : copilotOpen ? (
          <Copilot workflow={toWorkflow} descriptors={descriptors} onApply={applyOps} onClose={() => setCopilotOpen(false)} />
        ) : null}
      </div>
    </div>
  );
}

export default function Editor({ workflow }: { workflow: Workflow }) {
  return (
    <ReactFlowProvider>
      <EditorInner workflow={workflow} />
    </ReactFlowProvider>
  );
}
