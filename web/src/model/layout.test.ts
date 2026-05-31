import { describe, it, expect } from 'vitest';
import { autoLayout } from './layout';
import type { EditorNode, EditorEdge } from './graph';

const nodes: EditorNode[] = [
  { id: 'a', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'llm', config: {}, isStart: true } },
  { id: 'b', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'end', config: {}, isStart: false } },
];
const edges: EditorEdge[] = [
  { id: 'e0', source: 'a', target: 'b', data: { priority: 0, condition: '' } },
];

describe('autoLayout', () => {
  it('assigns finite, distinct positions', () => {
    const out = autoLayout(nodes, edges);
    expect(out).toHaveLength(2);
    for (const n of out) {
      expect(Number.isFinite(n.position.x)).toBe(true);
      expect(Number.isFinite(n.position.y)).toBe(true);
    }
    const a = out.find((n) => n.id === 'a')!;
    const b = out.find((n) => n.id === 'b')!;
    expect(b.position.x).toBeGreaterThan(a.position.x);
  });

  it('preserves node data', () => {
    const out = autoLayout(nodes, edges);
    expect(out.find((n) => n.id === 'a')!.data.isStart).toBe(true);
  });
});
