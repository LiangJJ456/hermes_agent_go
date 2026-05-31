// --- Wire types: mirror the backend (sub-project A) JSON shapes. ---

export type FieldType = 'string' | 'number' | 'bool' | 'string[]' | 'raw';

export interface FieldSchema {
  name: string;
  jsonName: string;
  type: FieldType;
  optional: boolean;
}

export interface NodeTypeSchema {
  type: string;
  fields: FieldSchema[];
}

export interface ValidationError {
  path: string;
  message: string;
}

export interface ValidateResponse {
  valid: boolean;
  errors: ValidationError[];
}

// Graph JSON uses PascalCase keys (backend contract).
export interface WireNode {
  Type: string;
  Config?: Record<string, unknown>;
}

export interface WireEdge {
  From: string;
  To: string;
  Condition?: string;
  Priority: number;
  Label?: string;
}

export interface WireGraph {
  StartAt: string;
  Nodes: Record<string, WireNode>;
  Edges: WireEdge[];
  MaxSteps?: number;
}

// --- Editor (canvas) data carried on React Flow nodes/edges. ---
// Index signatures satisfy React Flow's Record<string, unknown> data constraint.

export interface NodeData {
  nodeType: string; // e.g. "llm"
  config: Record<string, unknown>; // PascalCase config values
  isStart: boolean;
  [key: string]: unknown;
}

export interface EdgeData {
  priority: number;
  condition: string;
  [key: string]: unknown;
}
