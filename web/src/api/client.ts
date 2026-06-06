import type { NodeTypeSchema, ValidateResponse, WireGraph, ValidationError } from '../model/types';

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

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export interface GenerateResult {
  graph: WireGraph;
  valid: boolean;
  errors: ValidationError[];
  attempts: number;
  notes?: string;
}

// POST /api/generate. Sends the instruction plus the current canvas graph
// (omitted when undefined → from-scratch). Throws ApiError(status) on non-2xx
// so callers can distinguish 503 (no model) / 502 / 422.
export async function generateGraph(instruction: string, graph?: WireGraph): Promise<GenerateResult> {
  const body: { instruction: string; graph?: WireGraph } = { instruction };
  if (graph) body.graph = graph;
  const r = await fetch('/api/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    let msg = `/api/generate: HTTP ${r.status}`;
    try {
      const j = await r.json();
      if (j?.error) msg = j.error;
    } catch {
      // non-JSON body; keep default message
    }
    throw new ApiError(r.status, msg);
  }
  return (await r.json()) as GenerateResult;
}
