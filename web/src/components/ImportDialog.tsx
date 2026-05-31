import { useState } from 'react';
import type { WireGraph } from '../model/types';

interface Props {
  onImport: (graph: WireGraph) => void;
  onClose: () => void;
}

export function ImportDialog({ onImport, onClose }: Props) {
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);

  function doImport() {
    try {
      const parsed = JSON.parse(text) as WireGraph;
      setError(null);
      onImport(parsed);
    } catch {
      setError('Invalid JSON');
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>Import graph</h3>
        <textarea
          aria-label="Graph JSON"
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={12}
          placeholder='{"StartAt": "...", "Nodes": {...}, "Edges": [...]}'
        />
        {error && <div className="field-error">{error}</div>}
        <div className="modal-actions">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" onClick={doImport}>
            Import
          </button>
        </div>
      </div>
    </div>
  );
}
