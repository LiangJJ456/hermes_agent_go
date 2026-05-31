import { createContext, useContext, useState, type ReactNode } from 'react';
import type { NodeTypeSchema, ValidationError } from '../model/types';

// Named EditorSelection (not Selection) to avoid shadowing the DOM global `Selection`.
export type EditorSelection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null;

interface EditorState {
  schemas: NodeTypeSchema[];
  schemaFor: (nodeType: string) => NodeTypeSchema | undefined;
  selection: EditorSelection;
  setSelection: (s: EditorSelection) => void;
  errors: ValidationError[];
  setErrors: (e: ValidationError[]) => void;
}

const Ctx = createContext<EditorState | null>(null);

export function EditorProvider({ schemas, children }: { schemas: NodeTypeSchema[]; children: ReactNode }) {
  const [selection, setSelection] = useState<EditorSelection>(null);
  const [errors, setErrors] = useState<ValidationError[]>([]);
  const schemaFor = (t: string) => schemas.find((s) => s.type === t);
  return (
    <Ctx.Provider value={{ schemas, schemaFor, selection, setSelection, errors, setErrors }}>
      {children}
    </Ctx.Provider>
  );
}

export function useEditor(): EditorState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useEditor must be used within EditorProvider');
  return v;
}
