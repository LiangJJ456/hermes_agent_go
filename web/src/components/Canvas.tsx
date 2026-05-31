import { useCallback, useMemo } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  type Connection,
  type NodeChange,
  type EdgeChange,
  type OnSelectionChangeParams,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { NodeCard } from './NodeCard';
import { NODE_TYPE_MIME } from './Palette';
import type { EditorNode, EditorEdge } from '../model/graph';
import type { EditorSelection } from '../state/EditorContext';

interface Props {
  nodes: EditorNode[];
  edges: EditorEdge[];
  onNodesChange: (c: NodeChange[]) => void;
  onEdgesChange: (c: EdgeChange[]) => void;
  onConnect: (c: Connection) => void;
  onDropNode: (nodeType: string, position: { x: number; y: number }) => void;
  onSelect: (sel: EditorSelection) => void;
}

export function Canvas({ nodes, edges, onNodesChange, onEdgesChange, onConnect, onDropNode, onSelect }: Props) {
  const nodeTypes = useMemo(() => ({ hermes: NodeCard }), []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const nodeType = e.dataTransfer.getData(NODE_TYPE_MIME);
      if (!nodeType) return;
      const bounds = e.currentTarget.getBoundingClientRect();
      onDropNode(nodeType, { x: e.clientX - bounds.left, y: e.clientY - bounds.top });
    },
    [onDropNode],
  );

  const onSelectionChange = useCallback(
    (p: OnSelectionChangeParams) => {
      if (p.nodes.length > 0) onSelect({ kind: 'node', id: p.nodes[0].id });
      else if (p.edges.length > 0) onSelect({ kind: 'edge', id: p.edges[0].id });
      else onSelect(null);
    },
    [onSelect],
  );

  return (
    <div className="canvas-wrap" onDrop={onDrop} onDragOver={(e) => e.preventDefault()}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onSelectionChange={onSelectionChange}
        deleteKeyCode={['Delete', 'Backspace']}
        fitView
      >
        <Background />
        <Controls />
      </ReactFlow>
    </div>
  );
}
