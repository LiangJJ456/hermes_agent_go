interface Props {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}
export function BoolField({ label, value, onChange }: Props) {
  return (
    <label className="field field-bool">
      <input type="checkbox" aria-label={label} checked={!!value} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  );
}
