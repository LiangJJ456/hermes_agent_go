import { useCallback, useState } from 'react';
import { ReactFlowProvider, useNodesState, useEdgesState, addEdge, applyNodeChanges, type Connection, type NodeChange, type EdgeChange } from '@xyflow/react';
import { validateGraph, generateGraph, ApiError } from './api/client';
import { AiPanel } from './components/AiPanel';
import { toWire, fromWire, removeSelection, type EditorNode, type EditorEdge } from './model/graph';
import { autoLayout } from './model/layout';
import { mapError, type ErrorTarget } from './model/errors';
import type { WireGraph } from './model/types';
import { useEditor } from './state/EditorContext';
import { Palette } from './components/Palette';
import { Canvas } from './components/Canvas';
import { Inspector } from './components/Inspector';
import { ErrorPanel } from './components/ErrorPanel';
import { ImportDialog } from './components/ImportDialog';

let idSeq = 1;

export function EditorShell() {
  const { schemas, selection, setSelection, errors, setErrors } = useEditor();
  const [nodes, setNodes, onNodesChange] = useNodesState<EditorNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<EditorEdge>([]);
  const [importing, setImporting] = useState(false);
  const [aiBusy, setAiBusy] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  const [aiNotes, setAiNotes] = useState<string | null>(null);
  const [aiSnapshot, setAiSnapshot] = useState<{ nodes: EditorNode[]; edges: EditorEdge[] } | null>(null);

  const errorNodeIds = new Set(
    errors
      .map((e) => mapError(e).target)
      .filter((t): t is { kind: 'node'; id: string } => t.kind === 'node')
      .map((t) => t.id),
  );
  const decoratedNodes = nodes.map((n) => ({ ...n, data: { ...n.data, _hasError: errorNodeIds.has(n.id) } }));

  const onConnect = useCallback(
    (c: Connection) => setEdges((eds) => addEdge({ ...c, data: { priority: 0, condition: '' } }, eds)),
    [setEdges],
  );

  const onDropNode = useCallback(
    (nodeType: string, position: { x: number; y: number }) => {
      const id = `${nodeType}_${idSeq++}`;
      setNodes((nds) => [
        ...nds,
        { id, type: 'hermes', position, data: { nodeType, config: {}, isStart: nds.length === 0 } },
      ]);
    },
    [setNodes],
  );

  const doImport = useCallback(
    (g: WireGraph) => {
      const { nodes: n, edges: e } = fromWire(g);
      setNodes(autoLayout(n, e));
      setEdges(e);
      setErrors([]);
      setImporting(false);
    },
    [setNodes, setEdges, setErrors],
  );

  const doExport = useCallback(() => {
    const blob = new Blob([JSON.stringify(toWire(nodes, edges), null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'graph.json';
    a.click();
    URL.revokeObjectURL(url);
  }, [nodes, edges]);

  const doDeleteSelected = useCallback(() => {
    if (!selection) return;
    const out = removeSelection(nodes, edges, selection);
    setNodes(out.nodes);
    setEdges(out.edges);
    setSelection(null);
  }, [selection, nodes, edges, setNodes, setEdges, setSelection]);

  const doGenerate = useCallback(
    async (instruction: string) => {
      setAiBusy(true);
      setAiError(null);
      setAiNotes(null);
      try {
        const current = nodes.length > 0 ? toWire(nodes, edges) : undefined;
        const res = await generateGraph(instruction, current);
        setAiSnapshot({ nodes, edges }); // snapshot BEFORE replacing
        const { nodes: n, edges: e } = fromWire(res.graph);
        setNodes(autoLayout(n, e));
        setEdges(e);
        setErrors(res.errors);
        setSelection(null);
        setAiNotes(
          res.valid
            ? res.notes ?? `已生成(${n.length} 个节点)`
            : `已生成,但有 ${res.errors.length} 处问题,可手动修正或重试`,
        );
      } catch (err) {
        const status = err instanceof ApiError ? err.status : 0;
        if (status === 503) setAiError('未配置模型,请设置 OPENAI_API_KEY / HERMES_MODEL');
        else setAiError(`生成失败:${(err as Error).message}`);
      } finally {
        setAiBusy(false);
      }
    },
    [nodes, edges, setNodes, setEdges, setErrors, setSelection],
  );

  const doAiUndo = useCallback(() => {
    if (!aiSnapshot) return;
    setNodes(aiSnapshot.nodes);
    setEdges(aiSnapshot.edges);
    setErrors([]);
    setAiSnapshot(null);
    setAiNotes(null);
  }, [aiSnapshot, setNodes, setEdges, setErrors]);

  const doValidate = useCallback(async () => {
    try {
      const res = await validateGraph(toWire(nodes, edges));
      setErrors(res.errors);
    } catch (e) {
      alert(`Validate failed: ${(e as Error).message}`);
    }
  }, [nodes, edges, setErrors]);

  const focusTarget = useCallback(
    (t: ErrorTarget) => {
      if (t.kind === 'node') {
        setNodes((nds) =>
          applyNodeChanges(
            nds.map((n) => ({ type: 'select' as const, id: n.id, selected: n.id === t.id })),
            nds,
          ),
        );
        setSelection({ kind: 'node', id: t.id });
      } else if (t.kind === 'edge') {
        const e = edges[t.index];
        if (e) setSelection({ kind: 'edge', id: e.id });
      }
    },
    [edges, setNodes, setSelection],
  );

  const updateNodeData = useCallback(
    (id: string, patch: Partial<EditorNode['data']>) =>
      setNodes((nds) => nds.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n))),
    [setNodes],
  );
  const updateEdgeData = useCallback(
    (id: string, patch: Partial<NonNullable<EditorEdge['data']>>) =>
      setEdges((eds) => eds.map((e) => (e.id === id ? { ...e, data: { ...e.data!, ...patch } } : e))),
    [setEdges],
  );

  return (
    <div className="app">
      <header className="toolbar">
        <strong>Hermes Graph Editor</strong>
        <button onClick={() => setImporting(true)}>Import</button>
        <button onClick={doExport}>Export</button>
        <button onClick={doValidate}>Validate</button>
        <button onClick={doDeleteSelected} disabled={!selection}>
          Delete
        </button>
        <span className={errors.length ? 'errcount errcount-bad' : 'errcount'}>{errors.length} errors</span>
      </header>
      <div className="main">
        <Palette schemas={schemas} />
        <ReactFlowProvider>
          <Canvas
            nodes={decoratedNodes}
            edges={edges}
            onNodesChange={onNodesChange as (c: NodeChange[]) => void}
            onEdgesChange={onEdgesChange as (c: EdgeChange[]) => void}
            onConnect={onConnect}
            onDropNode={onDropNode}
            onSelect={setSelection}
          />
        </ReactFlowProvider>
        <div className="rightpane">
          <AiPanel
            onSubmit={doGenerate}
            busy={aiBusy}
            error={aiError}
            notes={aiNotes}
            canUndo={aiSnapshot !== null}
            onUndo={doAiUndo}
          />
          <Inspector
            selection={selection}
            nodes={nodes}
            edges={edges}
            schemas={schemas}
            onUpdateNode={updateNodeData}
            onUpdateEdge={updateEdgeData}
          />
          <ErrorPanel errors={errors} onFocus={focusTarget} />
        </div>
      </div>
      {importing && <ImportDialog onImport={doImport} onClose={() => setImporting(false)} />}
    </div>
  );
}
