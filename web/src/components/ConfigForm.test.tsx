import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ConfigForm } from './ConfigForm';
import { EdgeForm } from './EdgeForm';
import type { NodeTypeSchema } from '../model/types';

const llm: NodeTypeSchema = {
  type: 'llm',
  fields: [
    { name: 'Model', jsonName: 'Model', type: 'string', optional: false },
    { name: 'Tools', jsonName: 'Tools', type: 'string[]', optional: true },
    { name: 'OutputSchema', jsonName: 'OutputSchema', type: 'raw', optional: true },
    { name: 'MaxTokens', jsonName: 'MaxTokens', type: 'number', optional: true },
  ],
};

describe('ConfigForm', () => {
  it('renders a widget per schema field', () => {
    render(<ConfigForm schema={llm} config={{ Model: 'x' }} onChange={() => {}} />);
    expect(screen.getByLabelText('Model')).toBeInTheDocument();
    expect(screen.getByLabelText('Tools add item')).toBeInTheDocument();
    expect(screen.getByLabelText('OutputSchema')).toBeInTheDocument(); // raw -> textarea
    expect(screen.getByLabelText('MaxTokens')).toBeInTheDocument();
  });

  it('writes a changed field back through onChange', () => {
    const onChange = vi.fn();
    render(<ConfigForm schema={llm} config={{ Model: 'x' }} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'deepseek' } });
    expect(onChange).toHaveBeenCalledWith({ Model: 'deepseek' });
  });
});

describe('EdgeForm', () => {
  it('shows readonly endpoints and edits priority/condition', () => {
    const onChange = vi.fn();
    render(<EdgeForm from="a" to="b" priority={0} condition="" onChange={onChange} />);
    expect(screen.getByText('a → b')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '2' } });
    expect(onChange).toHaveBeenCalledWith({ priority: 2, condition: '' });
  });
});
