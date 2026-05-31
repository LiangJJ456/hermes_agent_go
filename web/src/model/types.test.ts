import { describe, it, expect } from 'vitest';
import type {
  FieldSchema,
  NodeTypeSchema,
  ValidateResponse,
  WireGraph,
  NodeData,
  EdgeData,
} from './types';

describe('types', () => {
  it('constructs each shape', () => {
    const f: FieldSchema = { name: 'Model', jsonName: 'Model', type: 'string', optional: false };
    const nt: NodeTypeSchema = { type: 'llm', fields: [f] };
    const vr: ValidateResponse = { valid: false, errors: [{ path: 'StartAt', message: 'x' }] };
    const g: WireGraph = { StartAt: 'a', Nodes: { a: { Type: 'end', Config: {} } }, Edges: [] };
    const nd: NodeData = { nodeType: 'llm', config: {}, isStart: true };
    const ed: EdgeData = { priority: 0, condition: '' };
    expect([nt, vr, g, nd, ed].length).toBe(5);
  });
});
