import { describe, it, expect, vi, afterEach } from 'vitest';
import { getNodeTypes, validateGraph } from './client';
import type { WireGraph } from '../model/types';

afterEach(() => vi.unstubAllGlobals());

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    })),
  );
}

describe('api client', () => {
  it('getNodeTypes returns parsed schemas', async () => {
    mockFetch(200, [{ type: 'llm', fields: [] }]);
    const schemas = await getNodeTypes();
    expect(schemas[0].type).toBe('llm');
    expect(fetch).toHaveBeenCalledWith('/api/nodetypes');
  });

  it('validateGraph posts the graph and parses the response', async () => {
    mockFetch(200, { valid: true, errors: [] });
    const g: WireGraph = { StartAt: 'a', Nodes: {}, Edges: [] };
    const res = await validateGraph(g);
    expect(res.valid).toBe(true);
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/api/validate');
    expect(call[1].method).toBe('POST');
    expect(JSON.parse(call[1].body)).toEqual(g);
  });

  it('throws on non-ok (non-validation) HTTP status', async () => {
    mockFetch(500, {});
    await expect(getNodeTypes()).rejects.toThrow(/500/);
  });
});
