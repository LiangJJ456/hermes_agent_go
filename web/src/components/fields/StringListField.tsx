import { useState } from 'react';

interface Props {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
}
export function StringListField({ label, value, onChange }: Props) {
  const [draft, setDraft] = useState('');
  const items = value ?? [];
  return (
    <div className="field">
      <span>{label}</span>
      <ul className="taglist">
        {items.map((it, i) => (
          <li key={i}>
            {it}
            <button type="button" onClick={() => onChange(items.filter((_, j) => j !== i))}>
              ×
            </button>
          </li>
        ))}
      </ul>
      <div className="tagadd">
        <input aria-label={`${label} add item`} value={draft} onChange={(e) => setDraft(e.target.value)} />
        <button
          type="button"
          onClick={() => {
            if (draft.trim()) {
              onChange([...items, draft.trim()]);
              setDraft('');
            }
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}
