'use client';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
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
import { Link } from '@/components/ui/link';
import * as Icons from 'lucide-react';
import { ArrowLeft, Sparkles, History, Save, Play, Keyboard } from 'lucide-react';
import type { NodeDescriptor } from '@crosscraft/schema';
import type { GraphOp, StepRecord, Workflow } from '@crosscraft/schema';
import { api } from '@/lib/client';
import { brand } from '@/lib/brand';
import { groupVar } from '@/lib/ui';
import { toast } from '@/components/ui/sonner';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '@/components/ui/resizable';
import { CcNode, type CcNodeData } from './CcNode';
import { Inspector } from './Inspector';
import { Palette } from './Palette';
import { Copilot } from './Copilot';
import { NodeRunOutput } from './NodeRunOutput';
import { ResumeDialog } from './ResumeDialog';
import { RunStatusPill } from './editor/RunStatusPill';
import { ShortcutsDialog } from './editor/ShortcutsDialog';
import { EmptyCanvas } from './editor/EmptyCanvas';

const nodeTypes: NodeTypes = { cc: CcNode };

interface RunState {
  status: 'idle' | 'running' | 'waiting' | 'success' | 'error';
  executionId?: string;
  waitingNodeId?: string;
}

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
  const [run, setRun] = useState<RunState>({ status: 'idle' });
  const [steps, setSteps] = useState<Record<string, StepRecord>>({});
  const [copilotOpen, setCopilotOpen] = useState(false);
  const [dockTab, setDockTab] = useState<'config' | 'run'>('config');
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition } = useReactFlow();

  // Refs mirror live state for keyboard handlers + undo history (avoid stale closures).
  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const edgesRef = useRef(edges);
  edgesRef.current = edges;
  const past = useRef<{ nodes: Node[]; edges: Edge[] }[]>([]);
  const future = useRef<{ nodes: Node[]; edges: Edge[] }[]>([]);
  const clipboard = useRef<Node[]>([]);

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

  // ── Undo / redo ──────────────────────────────────────────────────────────
  const record = useCallback(() => {
    past.current.push({ nodes: nodesRef.current, edges: edgesRef.current });
    if (past.current.length > 60) past.current.shift();
    future.current = [];
  }, []);
  const undo = useCallback(() => {
    const prev = past.current.pop();
    if (!prev) return;
    future.current.push({ nodes: nodesRef.current, edges: edgesRef.current });
    setNodes(prev.nodes);
    setEdges(prev.edges);
  }, [setNodes, setEdges]);
  const redo = useCallback(() => {
    const next = future.current.pop();
    if (!next) return;
    past.current.push({ nodes: nodesRef.current, edges: edgesRef.current });
    setNodes(next.nodes);
    setEdges(next.edges);
  }, [setNodes, setEdges]);

  const onConnect = useCallback(
    (c: Connection) => {
      record();
      setEdges((eds) => addEdge({ ...c, id: nanoid() }, eds));
    },
    [setEdges, record],
  );

  const addAtPosition = useCallback(
    (type: string, position: { x: number; y: number }) => {
      const desc = byType.get(type);
      if (!desc) return;
      record();
      setNodes((nds) => [...nds, makeFlowNode(desc, { position })]);
    },
    [byType, setNodes, record],
  );

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const type = e.dataTransfer.getData('application/cc-node');
      addAtPosition(type, screenToFlowPosition({ x: e.clientX, y: e.clientY }));
    },
    [addAtPosition, screenToFlowPosition],
  );

  const addAtCenter = useCallback(
    (type: string) => {
      const rect = wrapper.current?.getBoundingClientRect();
      const center = rect
        ? screenToFlowPosition({ x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 })
        : { x: 200, y: 160 };
      addAtPosition(type, { x: center.x - 90, y: center.y - 24 });
    },
    [addAtPosition, screenToFlowPosition],
  );

  // ── Clipboard: copy / paste / duplicate ───────────────────────────────────
  const copy = useCallback(() => {
    clipboard.current = nodesRef.current.filter((n) => n.selected);
  }, []);

  const pasteNodes = useCallback(
    (source: Node[]) => {
      if (!source.length) return;
      record();
      const idMap = new Map<string, string>();
      const clones = source.map((n) => {
        const id = nanoid();
        idMap.set(n.id, id);
        return {
          ...n,
          id,
          position: { x: n.position.x + 48, y: n.position.y + 48 },
          selected: true,
          data: { ...(n.data as CcNodeData), params: { ...(n.data as CcNodeData).params } },
        } as Node;
      });
      // carry over edges fully contained within the copied selection
      const innerEdges = edgesRef.current
        .filter((e) => idMap.has(e.source) && idMap.has(e.target))
        .map((e) => ({ ...e, id: nanoid(), source: idMap.get(e.source)!, target: idMap.get(e.target)! }));
      setNodes((nds) => [...nds.map((n) => ({ ...n, selected: false })), ...clones]);
      setEdges((eds) => [...eds, ...innerEdges]);
    },
    [setNodes, setEdges, record],
  );

  const paste = useCallback(() => pasteNodes(clipboard.current), [pasteNodes]);
  const duplicate = useCallback(() => pasteNodes(nodesRef.current.filter((n) => n.selected)), [pasteNodes]);

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
    try {
      await api.saveWorkflow(toWorkflow());
      toast.success('Workflow saved');
    } catch (e) {
      toast.error('Save failed', { description: (e as Error).message });
    }
  }, [toWorkflow]);

  // ── Run + live monitoring (SSE) ───────────────────────────────────────────
  const watch = useCallback(
    (executionId: string) => {
      const es = new EventSource(`/api/executions/${executionId}/stream`);
      es.onmessage = (ev) => {
        const data = JSON.parse(ev.data) as { status: RunState['status']; waitingNodeId?: string; steps: StepRecord[] };
        const map: Record<string, StepRecord> = {};
        for (const s of data.steps) map[s.nodeId] = s;
        setSteps(map);
        setRun({ status: data.status, executionId, waitingNodeId: data.waitingNodeId });
        setNodes((nds) =>
          nds.map((n) => {
            const st = map[n.id]?.status;
            const isWaiting = data.status === 'waiting' && data.waitingNodeId === n.id;
            return { ...n, data: { ...n.data, status: isWaiting ? 'waiting' : st } };
          }),
        );
        if (data.status === 'waiting') toast.info('Run is waiting', { description: 'Resume it from the top bar.' });
        if (data.status === 'success') {
          toast.success('Run completed');
          es.close();
        }
        if (data.status === 'error') {
          toast.error('Run failed');
          es.close();
        }
      };
      es.onerror = () => es.close();
    },
    [setNodes],
  );

  const doRun = useCallback(async () => {
    try {
      await api.saveWorkflow(toWorkflow());
    } catch (e) {
      toast.error('Save failed', { description: (e as Error).message });
      return;
    }
    setSteps({});
    setNodes((nds) => nds.map((n) => ({ ...n, data: { ...n.data, status: undefined } })));
    setRun({ status: 'running' });
    try {
      const { executionId } = await api.run(workflow.id, {});
      setRun({ status: 'running', executionId });
      watch(executionId);
    } catch (e) {
      setRun({ status: 'error' });
      toast.error('Run failed', { description: (e as Error).message });
    }
  }, [toWorkflow, workflow.id, watch, setNodes]);

  const applyOps = useCallback(
    (ops: GraphOp[]) => {
      record();
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
              {
                id: op.edge.id || nanoid(),
                source: op.edge.source,
                target: op.edge.target,
                sourceHandle: op.edge.sourceHandle ?? 'main',
                targetHandle: op.edge.targetHandle ?? 'main',
              },
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
    [byType, setNodes, setEdges, record],
  );

  // ── Keyboard shortcuts ─────────────────────────────────────────────────────
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      const typing = el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable;
      const mod = e.ctrlKey || e.metaKey;
      const k = e.key.toLowerCase();
      if (mod && k === 'z') {
        e.preventDefault();
        e.shiftKey ? redo() : undo();
      } else if (mod && k === 'y') {
        e.preventDefault();
        redo();
      } else if (mod && k === 's') {
        e.preventDefault();
        save();
      } else if (mod && k === 'c' && !typing) {
        copy();
      } else if (mod && k === 'v' && !typing) {
        paste();
      } else if (mod && k === 'd' && !typing) {
        e.preventDefault();
        duplicate();
      } else if ((e.key === 'Delete' || e.key === 'Backspace') && !typing) {
        record(); // snapshot so the delete React Flow is about to do is undoable
      } else if (e.key === '?' && !typing) {
        setShortcutsOpen(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [undo, redo, save, copy, paste, duplicate, record]);

  const selNode = nodes.find((n) => n.id === selected);
  const selDesc = selNode ? (selNode.data as CcNodeData).descriptor : null;
  const dockOpen = Boolean((selNode && selDesc) || copilotOpen);
  const LogoIcon = (Icons as Record<string, unknown>)[brand.logoIcon] as
    | React.ComponentType<{ className?: string }>
    | undefined;

  return (
    <div className="flex h-screen flex-col bg-bg">
      {/* ── Top bar ── */}
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to workflows">
          <Link href="/">
            <ArrowLeft />
          </Link>
        </Button>
        <span className="flex items-center gap-1.5 pl-1 pr-2 text-accent-2">
          {LogoIcon && <LogoIcon className="size-4" />}
        </span>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="min-w-0 max-w-xs flex-1 truncate rounded-md bg-transparent px-1.5 py-1 text-[15px] font-semibold text-text outline-none hover:bg-panel-2 focus:bg-panel-2 focus:ring-2 focus:ring-ring"
          aria-label="Workflow name"
        />
        <RunStatusPill status={run.status} />
        <div className="flex-1" />

        {run.status === 'waiting' && run.executionId && (
          <ResumeDialog executionId={run.executionId} />
        )}

        <label className="flex cursor-pointer items-center gap-2 px-1 text-xs text-muted">
          <Switch checked={active} onCheckedChange={setActive} />
          Active
        </label>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" onClick={() => setShortcutsOpen(true)} aria-label="Keyboard shortcuts">
              <Keyboard />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Keyboard shortcuts (?)</TooltipContent>
        </Tooltip>

        <Button
          variant={copilotOpen ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setCopilotOpen((v) => !v)}
        >
          <Sparkles />
          Copilot
        </Button>
        <Button asChild variant="ghost" size="sm">
          <Link href={`/executions/${workflow.id}`}>
            <History />
            Runs
          </Link>
        </Button>
        <Button variant="secondary" size="sm" onClick={save}>
          <Save />
          Save
        </Button>
        <Button size="sm" onClick={doRun} disabled={run.status === 'running'}>
          <Play />
          Run
        </Button>
      </header>

      {/* ── Body ── */}
      <div className="flex min-h-0 flex-1">
        <Palette descriptors={descriptors} onAdd={addAtCenter} />

        <ResizablePanelGroup direction="horizontal" key={dockOpen ? 'dock' : 'nodock'} className="min-w-0 flex-1">
          <ResizablePanel order={1} defaultSize={74} minSize={30} className="min-w-0">
            <div ref={wrapper} className="relative h-full" onDrop={onDrop} onDragOver={(e) => e.preventDefault()}>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                nodeTypes={nodeTypes}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={onConnect}
                onNodeClick={(_, n) => {
                  setSelected(n.id);
                  setDockTab('config');
                }}
                onNodeDragStart={record}
                onPaneClick={() => setSelected(null)}
                deleteKeyCode={['Delete', 'Backspace']}
                multiSelectionKeyCode={['Shift', 'Meta', 'Control']}
                selectionKeyCode="Shift"
                fitView
                proOptions={{ hideAttribution: true }}
              >
                <Background color="var(--border-2)" gap={18} />
                <Controls />
                <MiniMap
                  pannable
                  zoomable
                  maskColor="rgba(244,243,238,0.85)"
                  nodeColor={(n) => groupVar((n.data as CcNodeData).descriptor?.group ?? 'transform')}
                  nodeStrokeWidth={2}
                />
              </ReactFlow>
              {nodes.length === 0 && <EmptyCanvas />}
            </div>
          </ResizablePanel>

          {dockOpen && (
            <>
              <ResizableHandle withHandle />
              <ResizablePanel order={2} defaultSize={26} minSize={20} maxSize={48} className="bg-panel">
                <Dock
                  selNode={selNode}
                  selDesc={selDesc}
                  dockTab={dockTab}
                  setDockTab={setDockTab}
                  step={selNode ? steps[selNode.id] : undefined}
                  copilotOpen={copilotOpen}
                  onRename={(nm) =>
                    setNodes((nds) => nds.map((n) => (n.id === selNode!.id ? { ...n, data: { ...n.data, label: nm } } : n)))
                  }
                  onChangeParams={(params) =>
                    setNodes((nds) => nds.map((n) => (n.id === selNode!.id ? { ...n, data: { ...n.data, params } } : n)))
                  }
                  onDelete={() => {
                    record();
                    setNodes((nds) => nds.filter((n) => n.id !== selNode!.id));
                    setEdges((eds) => eds.filter((e) => e.source !== selNode!.id && e.target !== selNode!.id));
                    setSelected(null);
                  }}
                  copilot={
                    <Copilot
                      workflow={toWorkflow}
                      descriptors={descriptors}
                      onApply={applyOps}
                      onClose={() => setCopilotOpen(false)}
                    />
                  }
                />
              </ResizablePanel>
            </>
          )}
        </ResizablePanelGroup>
      </div>

      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </div>
  );
}

/** Right dock: node context (Config/Run tabs) above an optional Copilot split. */
function Dock({
  selNode,
  selDesc,
  dockTab,
  setDockTab,
  step,
  copilotOpen,
  onRename,
  onChangeParams,
  onDelete,
  copilot,
}: {
  selNode?: Node;
  selDesc: NodeDescriptor | null;
  dockTab: 'config' | 'run';
  setDockTab: (t: 'config' | 'run') => void;
  step?: StepRecord;
  copilotOpen: boolean;
  onRename: (name: string) => void;
  onChangeParams: (params: Record<string, unknown>) => void;
  onDelete: () => void;
  copilot: React.ReactNode;
}) {
  const context =
    selNode && selDesc ? (
      <Tabs value={dockTab} onValueChange={(v) => setDockTab(v as 'config' | 'run')} className="flex h-full flex-col">
        <div className="border-b border-border px-3 py-2">
          <TabsList className="w-full">
            <TabsTrigger value="config" className="flex-1">
              Config
            </TabsTrigger>
            <TabsTrigger value="run" className="flex-1">
              Run output
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="config" className="min-h-0 flex-1 overflow-hidden">
          <Inspector
            descriptor={selDesc}
            name={(selNode.data as CcNodeData).label}
            params={(selNode.data as CcNodeData).params}
            onRename={onRename}
            onChange={onChangeParams}
            onDelete={onDelete}
          />
        </TabsContent>
        <TabsContent value="run" className="min-h-0 flex-1 overflow-y-auto">
          <NodeRunOutput step={step} />
        </TabsContent>
      </Tabs>
    ) : null;

  if (context && copilotOpen) {
    return (
      <ResizablePanelGroup direction="vertical">
        <ResizablePanel defaultSize={60} minSize={25}>
          {context}
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={40} minSize={20}>
          {copilot}
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }
  return <div className="h-full">{context ?? copilot}</div>;
}

export default function Editor({ workflow }: { workflow: Workflow }) {
  return (
    <ReactFlowProvider>
      <EditorInner workflow={workflow} />
    </ReactFlowProvider>
  );
}
