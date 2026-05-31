import type { NodeTypeSchema, FieldSchema } from '../model/types';
import { StringField } from './fields/StringField';
import { NumberField } from './fields/NumberField';
import { BoolField } from './fields/BoolField';
import { StringListField } from './fields/StringListField';
import { RawJsonField } from './fields/RawJsonField';

interface Props {
  schema: NodeTypeSchema;
  config: Record<string, unknown>;
  onChange: (config: Record<string, unknown>) => void;
}

export function ConfigForm({ schema, config, onChange }: Props) {
  const set = (key: string, v: unknown) => onChange({ ...config, [key]: v });

  return (
    <div className="configform">
      {schema.fields.map((f) => (
        <FieldWidget key={f.jsonName} field={f} value={config[f.jsonName]} onChange={(v) => set(f.jsonName, v)} />
      ))}
    </div>
  );
}

function FieldWidget({ field, value, onChange }: { field: FieldSchema; value: unknown; onChange: (v: unknown) => void }) {
  switch (field.type) {
    case 'string':
      return <StringField label={field.jsonName} value={(value as string) ?? ''} onChange={onChange} />;
    case 'number':
      return <NumberField label={field.jsonName} value={(value as number) ?? 0} onChange={onChange} />;
    case 'bool':
      return <BoolField label={field.jsonName} value={(value as boolean) ?? false} onChange={onChange} />;
    case 'string[]':
      return <StringListField label={field.jsonName} value={(value as string[]) ?? []} onChange={onChange} />;
    case 'raw':
    default:
      return <RawJsonField label={field.jsonName} value={value} onChange={onChange} />;
  }
}
