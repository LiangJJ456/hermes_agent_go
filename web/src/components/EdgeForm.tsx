import { NumberField } from './fields/NumberField';
import { StringField } from './fields/StringField';

interface Props {
  from: string;
  to: string;
  priority: number;
  condition: string;
  onChange: (v: { priority: number; condition: string }) => void;
}

export function EdgeForm({ from, to, priority, condition, onChange }: Props) {
  return (
    <div className="edgeform">
      <div className="edge-endpoints">
        {from} → {to}
      </div>
      <NumberField label="Priority" value={priority} onChange={(p) => onChange({ priority: p, condition })} />
      <StringField label="Condition" value={condition} onChange={(c) => onChange({ priority, condition: c })} />
      <p className="hint">Condition DSL: e.g. <code>input.has_tool_calls == true</code>. Empty = unconditional.</p>
    </div>
  );
}
