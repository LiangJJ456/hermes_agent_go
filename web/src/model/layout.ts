import dagre from '@dagrejs/dagre';
import type { EditorNode, EditorEdge } from './graph';

const NODE_W = 180;
const NODE_H = 64;

// Assign positions via a left-to-right dagre layout. React Flow positions are
// top-left corners; dagre returns node centers, so we offset by half-size.
export function autoLayout(nodes: EditorNode[], edges: EditorEdge[]): EditorNode[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: 'LR', nodesep: 40, ranksep: 90 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const n of nodes) g.setNode(n.id, { width: NODE_W, height: NODE_H });
  for (const e of edges) {
    if (g.hasNode(e.source) && g.hasNode(e.target)) g.setEdge(e.source, e.target);
  }

  dagre.layout(g);

  return nodes.map((n) => {
    const p = g.node(n.id);
    return { ...n, position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } };
  });
}
