import type { ValidationError } from './types';

export type ErrorTarget =
  | { kind: 'node'; id: string }
  | { kind: 'edge'; index: number }
  | { kind: 'graph' };

export interface MappedError {
  error: ValidationError;
  target: ErrorTarget;
}

// Map a validation error to a canvas element. Precise paths (edges[N].*) map
// directly. Parse-level errors arrive as path "<graph>" with the location only
// in the message text (backend contract), so we best-effort parse node/edge
// references from the message. Anything unlocatable maps to the graph (panel).
export function mapError(e: ValidationError): MappedError {
  const edgePath = e.path.match(/^edges\[(\d+)\]/);
  if (edgePath) return { error: e, target: { kind: 'edge', index: Number(edgePath[1]) } };

  const nodeMsg = e.message.match(/node "([^"]+)"/);
  if (nodeMsg) return { error: e, target: { kind: 'node', id: nodeMsg[1] } };

  const edgeMsg = e.message.match(/edge (\d+)/);
  if (edgeMsg) return { error: e, target: { kind: 'edge', index: Number(edgeMsg[1]) } };

  return { error: e, target: { kind: 'graph' } };
}
