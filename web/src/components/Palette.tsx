import type { NodeTypeSchema } from '../model/types';

export const NODE_TYPE_MIME = 'application/hermes-node-type';

export function Palette({ schemas }: { schemas: NodeTypeSchema[] }) {
  return (
    <aside className="palette">
      <div className="palette-title">Nodes</div>
      {schemas.map((s) => (
        <div
          key={s.type}
          className="palette-item"
          draggable
          onDragStart={(e) => e.dataTransfer.setData(NODE_TYPE_MIME, s.type)}
        >
          {s.type}
        </div>
      ))}
    </aside>
  );
}
