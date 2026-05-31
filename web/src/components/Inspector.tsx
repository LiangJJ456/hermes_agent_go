import type { NodeTypeSchema } from '../model/types';
import type { EditorNode, EditorEdge } from '../model/graph';
import type { EditorSelection } from '../state/EditorContext';
import { ConfigForm } from './ConfigForm';
import { EdgeForm } from './EdgeForm';

interface Props {
  selection: EditorSelection;
  nodes: EditorNode[];
  edges: EditorEdge[];
  schemas: NodeTypeSchema[];
  onUpdateNode: (id: string, patch: Partial<EditorNode['data']>) => void;
  onUpdateEdge: (id: string, patch: Partial<NonNullable<EditorEdge['data']>>) => void;
}

export function Inspector({ selection, nodes, edges, schemas, onUpdateNode, onUpdateEdge }: Props) {
  if (!selection) return <div className="inspector inspector-empty">Select a node or edge</div>;

  if (selection.kind === 'node') {
    const node = nodes.find((n) => n.id === selection.id);
    if (!node) return <div className="inspector inspector-empty">Node not found</div>;
    const schema = schemas.find((s) => s.type === node.data.nodeType);
    return (
      <div className="inspector">
        <div className="inspector-title">{node.id} <em>({node.data.nodeType})</em></div>
        {schema ? (
          <ConfigForm
            key={node.id}
            schema={schema}
            config={node.data.config}
            onChange={(config) => onUpdateNode(node.id, { config })}
          />
        ) : (
          <p>No schema for "{node.data.nodeType}"</p>
        )}
      </div>
    );
  }

  const edge = edges.find((e) => e.id === selection.id);
  if (!edge) return <div className="inspector inspector-empty">Edge not found</div>;
  return (
    <div className="inspector">
      <div className="inspector-title">Edge</div>
      <EdgeForm
        from={edge.source}
        to={edge.target}
        priority={edge.data?.priority ?? 0}
        condition={edge.data?.condition ?? ''}
        onChange={(v) => onUpdateEdge(edge.id, v)}
      />
    </div>
  );
}
