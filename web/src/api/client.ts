import type { NodeTypeSchema, ValidateResponse, WireGraph } from '../model/types';

export async function getNodeTypes(): Promise<NodeTypeSchema[]> {
  const r = await fetch('/api/nodetypes');
  if (!r.ok) throw new Error(`/api/nodetypes: HTTP ${r.status}`);
  return (await r.json()) as NodeTypeSchema[];
}

// Note: an invalid graph still returns HTTP 200 with valid:false (backend
// contract). Only transport / non-2xx (e.g. 400 non-JSON, 500) throws.
export async function validateGraph(graph: WireGraph): Promise<ValidateResponse> {
  const r = await fetch('/api/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(graph),
  });
  if (!r.ok) throw new Error(`/api/validate: HTTP ${r.status}`);
  return (await r.json()) as ValidateResponse;
}
