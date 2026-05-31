import { describe, it, expect } from 'vitest';
import { mapError } from './errors';

describe('mapError', () => {
  it('maps precise edge paths to that edge', () => {
    const m = mapError({ path: 'edges[2].to', message: 'edge 2: To references unknown node "ghost"' });
    expect(m.target).toEqual({ kind: 'edge', index: 2 });
  });

  it('best-effort maps "<graph>" node messages to the node', () => {
    const m = mapError({ path: '<graph>', message: 'node "search": validate config: resource required' });
    expect(m.target).toEqual({ kind: 'node', id: 'search' });
  });

  it('best-effort maps "<graph>" edge messages to the edge', () => {
    const m = mapError({ path: '<graph>', message: 'edge 0 (a->b): condition: ...' });
    expect(m.target).toEqual({ kind: 'edge', index: 0 });
  });

  it('falls back to graph for unlocatable errors', () => {
    const m = mapError({ path: 'StartAt', message: 'StartAt is empty' });
    expect(m.target).toEqual({ kind: 'graph' });
  });
});
