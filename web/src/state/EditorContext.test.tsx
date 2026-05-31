import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { EditorProvider, useEditor } from './EditorContext';

function Probe() {
  const { selection, setSelection } = useEditor();
  return (
    <div>
      <span data-testid="sel">{selection ? `${selection.kind}:${selection.id}` : 'none'}</span>
      <button onClick={() => setSelection({ kind: 'node', id: 'a' })}>select</button>
    </div>
  );
}

describe('EditorContext', () => {
  it('provides and updates selection', () => {
    render(
      <EditorProvider schemas={[]}>
        <Probe />
      </EditorProvider>,
    );
    expect(screen.getByTestId('sel').textContent).toBe('none');
    fireEvent.click(screen.getByText('select'));
    expect(screen.getByTestId('sel').textContent).toBe('node:a');
  });
});
