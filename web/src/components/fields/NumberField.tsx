interface Props {
  label: string;
  value: number;
  onChange: (v: number) => void;
}
export function NumberField({ label, value, onChange }: Props) {
  return (
    <label className="field">
      <span>{label}</span>
      <input
        type="number"
        aria-label={label}
        value={Number.isFinite(value) ? value : 0}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </label>
  );
}
