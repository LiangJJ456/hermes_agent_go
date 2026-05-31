import { useState } from 'react';

interface Props {
  label: string;
  value: unknown;
  onChange: (v: unknown) => void;
}
export function RawJsonField({ label, value, onChange }: Props) {
  const [text, setText] = useState(() => JSON.stringify(value ?? null, null, 2));
  const [error, setError] = useState<string | null>(null);

  function commit() {
    try {
      const parsed = JSON.parse(text);
      setError(null);
      onChange(parsed);
    } catch {
      setError('Invalid JSON');
    }
  }

  return (
    <div className="field">
      <span>{label}</span>
      <textarea aria-label={label} value={text} onChange={(e) => setText(e.target.value)} onBlur={commit} rows={4} />
      {error && <div className="field-error">{error}</div>}
    </div>
  );
}
