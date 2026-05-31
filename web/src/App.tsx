import { useEffect, useState } from 'react';
import './App.css';
import { getNodeTypes } from './api/client';
import type { NodeTypeSchema } from './model/types';
import { EditorProvider } from './state/EditorContext';
import { EditorShell } from './EditorShell';

// App is a thin loader + provider. State (selection/errors/schemas) lives in
// EditorContext; EditorShell (below the provider) consumes it.
export default function App() {
  const [schemas, setSchemas] = useState<NodeTypeSchema[]>([]);
  useEffect(() => {
    getNodeTypes()
      .then(setSchemas)
      .catch((e) => alert(`Failed to load node types: ${(e as Error).message}`));
  }, []);
  return (
    <EditorProvider schemas={schemas}>
      <EditorShell />
    </EditorProvider>
  );
}
