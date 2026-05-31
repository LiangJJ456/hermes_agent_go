import type { Node, Edge } from '@xyflow/react';
import type { WireGraph, WireNode, WireEdge, NodeData, EdgeData } from './types';

export type EditorNode = Node<NodeData>;
export type EditorEdge = Edge<EdgeData>;

// Canvas → wire (PascalCase). Positions are dropped (not part of the model).
export function toWire(nodes: EditorNode[], edges: EditorEdge[]): WireGraph {
  const Nodes: Record<string, WireNode> = {};
  let StartAt = '';
  for (const n of nodes) {
    Nodes[n.id] = { Type: n.data.nodeType, Config: n.data.config };
    if (n.data.isStart) StartAt = n.id;
  }
  const Edges: WireEdge[] = edges.map((e) => {
    const w: WireEdge = { From: e.source, To: e.target, Priority: e.data?.priority ?? 0 };
    if (e.data?.condition) w.Condition = e.data.condition;
    return w;
  });
  return { StartAt, Nodes, Edges };
}

// Remove the selected element. Deleting a node also drops every edge touching
// it (you can't have a dangling edge). Deleting an edge leaves nodes alone.
export function removeSelection(
  nodes: EditorNode[],
  edges: EditorEdge[],
  selection: { kind: 'node' | 'edge'; id: string },
): { nodes: EditorNode[]; edges: EditorEdge[] } {
  if (selection.kind === 'node') {
    return {
      nodes: nodes.filter((n) => n.id !== selection.id),
      edges: edges.filter((e) => e.source !== selection.id && e.target !== selection.id),
    };
  }
  return { nodes, edges: edges.filter((e) => e.id !== selection.id) };
}

// wire → canvas. Positions default to (0,0); autoLayout assigns real ones.
// v1 limitation: EdgeSpec.Label (display-only) is not modeled on EdgeData and
// is dropped on round-trip. Add a `label` field to EdgeData if the editor ever
// needs to preserve/display edge labels.
export function fromWire(g: WireGraph): { nodes: EditorNode[]; edges: EditorEdge[] } {
  const nodes: EditorNode[] = Object.entries(g.Nodes ?? {}).map(([id, n]) => ({
    id,
    type: 'hermes',
    position: { x: 0, y: 0 },
    data: { nodeType: n.Type, config: n.Config ?? {}, isStart: id === g.StartAt },
  }));
  const edges: EditorEdge[] = (g.Edges ?? []).map((e, i) => ({
    id: `e${i}-${e.From}-${e.To}`,
    source: e.From,
    target: e.To,
    data: { priority: e.Priority ?? 0, condition: e.Condition ?? '' },
  }));
  return { nodes, edges };
}
