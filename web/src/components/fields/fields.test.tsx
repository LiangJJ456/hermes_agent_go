import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { StringField } from './StringField';
import { NumberField } from './NumberField';
import { BoolField } from './BoolField';
import { StringListField } from './StringListField';
import { RawJsonField } from './RawJsonField';

describe('field widgets', () => {
  it('StringField calls onChange with text', () => {
    const onChange = vi.fn();
    render(<StringField label="Model" value="x" onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'deepseek' } });
    expect(onChange).toHaveBeenCalledWith('deepseek');
  });

  it('NumberField calls onChange with a number', () => {
    const onChange = vi.fn();
    render(<NumberField label="Priority" value={0} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '3' } });
    expect(onChange).toHaveBeenCalledWith(3);
  });

  it('BoolField toggles', () => {
    const onChange = vi.fn();
    render(<BoolField label="Async" value={false} onChange={onChange} />);
    fireEvent.click(screen.getByLabelText('Async'));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('StringListField adds an item', () => {
    const onChange = vi.fn();
    render(<StringListField label="Tools" value={['a']} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Tools add item'), { target: { value: 'b' } });
    fireEvent.click(screen.getByText('Add'));
    expect(onChange).toHaveBeenCalledWith(['a', 'b']);
  });

  it('RawJsonField reports valid parsed JSON on blur', () => {
    const onChange = vi.fn();
    render(<RawJsonField label="Parameters" value={{ a: 1 }} onChange={onChange} />);
    const ta = screen.getByLabelText('Parameters');
    fireEvent.change(ta, { target: { value: '{"b":2}' } });
    fireEvent.blur(ta);
    expect(onChange).toHaveBeenCalledWith({ b: 2 });
  });

  it('RawJsonField marks invalid JSON and does not call onChange', () => {
    const onChange = vi.fn();
    render(<RawJsonField label="Parameters" value={{}} onChange={onChange} />);
    const ta = screen.getByLabelText('Parameters');
    fireEvent.change(ta, { target: { value: '{bad' } });
    fireEvent.blur(ta);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
  });
});
