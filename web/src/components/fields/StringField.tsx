interface Props {
  label: string;
  value: string;
  onChange: (v: string) => void;
}
export function StringField({ label, value, onChange }: Props) {
  return (
    <label className="field">
      <span>{label}</span>
      <input aria-label={label} value={value ?? ''} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}
