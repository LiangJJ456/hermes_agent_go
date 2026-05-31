import { describe, it, expect } from 'vitest';
import { toWire, fromWire } from './graph';
import type { WireGraph } from './types';

const wire: WireGraph = {
  StartAt: 'classify',
  Nodes: {
    classify: { Type: 'llm', Config: { Model: 'deepseek-v4', Tools: ['web.search'] } },
    route: { Type: 'choice', Config: { Choices: [{ Next: 'done', Condition: 'input.ok == true' }] } },
    done: { Type: 'end', Config: { Status: 'success' } },
  },
  Edges: [
    { From: 'classify', To: 'route', Priority: 0 },
    { From: 'route', To: 'done', Priority: 1, Condition: 'input.ok == true' },
  ],
};

describe('graph conversion', () => {
  it('fromWire produces canvas nodes/edges with PascalCase config preserved', () => {
    const { nodes, edges } = fromWire(wire);
    expect(nodes).toHaveLength(3);
    const classify = nodes.find((n) => n.id === 'classify')!;
    expect(classify.data.nodeType).toBe('llm');
    expect(classify.data.config.Model).toBe('deepseek-v4');
    expect(classify.data.isStart).toBe(true);
    expect(nodes.find((n) => n.id === 'route')!.data.isStart).toBe(false);
    expect(edges).toHaveLength(2);
    const e1 = edges.find((e) => e.source === 'route' && e.target === 'done')!;
    expect(e1.data!.priority).toBe(1);
    expect(e1.data!.condition).toBe('input.ok == true');
  });

  it('round-trips wire -> canvas -> wire without losing data', () => {
    const { nodes, edges } = fromWire(wire);
    const out = toWire(nodes, edges);
    expect(out.StartAt).toBe('classify');
    expect(Object.keys(out.Nodes).sort()).toEqual(['classify', 'done', 'route']);
    expect(out.Nodes.classify.Type).toBe('llm');
    expect(out.Nodes.classify.Config).toEqual({ Model: 'deepseek-v4', Tools: ['web.search'] });
    expect(out.Nodes.route.Config!.Choices).toEqual([{ Next: 'done', Condition: 'input.ok == true' }]);
    const edgeSet = out.Edges.map((e) => `${e.From}->${e.To}:${e.Priority}:${e.Condition ?? ''}`).sort();
    expect(edgeSet).toEqual(['classify->route:0:', 'route->done:1:input.ok == true']);
  });

  it('omits empty condition on export', () => {
    const { nodes, edges } = fromWire(wire);
    const out = toWire(nodes, edges);
    const e0 = out.Edges.find((e) => e.From === 'classify')!;
    expect('Condition' in e0).toBe(false);
  });
});
