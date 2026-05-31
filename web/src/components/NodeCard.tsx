import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { NodeData } from '../model/types';

const TYPE_COLORS: Record<string, string> = {
  llm: '#2563eb',
  tool: '#059669',
  choice: '#d97706',
  parallel: '#7c3aed',
  human: '#db2777',
  end: '#6b7280',
};

// One-line summary of the most important config field for this node type.
export function summarize(nodeType: string, config: Record<string, unknown>): string {
  switch (nodeType) {
    case 'llm':
      return String(config.Model ?? '');
    case 'tool':
      return String(config.Resource ?? '');
    case 'end':
      return String(config.Status ?? '');
    case 'choice': {
      const c = config.Choices;
      return Array.isArray(c) ? `${c.length} branches` : '';
    }
    case 'parallel': {
      const b = config.Branches;
      return Array.isArray(b) ? `${b.length} branches` : '';
    }
    default: {
      const first = Object.values(config).find((v) => typeof v === 'string');
      return first ? String(first) : '';
    }
  }
}

export function NodeCard({ id, data }: NodeProps) {
  const d = data as NodeData & { _hasError?: boolean };
  const color = TYPE_COLORS[d.nodeType] ?? '#6b7280';
  const summary = summarize(d.nodeType, d.config);
  return (
    <div className="nodecard" style={{ borderColor: color }}>
      <Handle type="target" position={Position.Left} />
      {d._hasError && (
        <span className="nodecard-badge" aria-label="has errors">
          !
        </span>
      )}
      <div className="nodecard-head" style={{ background: color }}>
        <span className="nodecard-id">{id}</span>
        <span className="nodecard-type">{d.nodeType}</span>
        {d.isStart && <span className="nodecard-start">▶</span>}
      </div>
      {summary && <div className="nodecard-summary">{summary}</div>}
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
