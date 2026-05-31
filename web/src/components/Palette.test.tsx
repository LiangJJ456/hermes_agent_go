import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Palette } from './Palette';
import type { NodeTypeSchema } from '../model/types';

const schemas: NodeTypeSchema[] = [
  { type: 'llm', fields: [] },
  { type: 'tool', fields: [] },
  { type: 'end', fields: [] },
];

describe('Palette', () => {
  it('lists one draggable item per node type', () => {
    render(<Palette schemas={schemas} />);
    for (const t of ['llm', 'tool', 'end']) {
      const item = screen.getByText(t);
      expect(item).toBeInTheDocument();
      expect(item.closest('[draggable="true"]')).not.toBeNull();
    }
  });

  it('sets the node type on dragstart', () => {
    render(<Palette schemas={schemas} />);
    const item = screen.getByText('llm').closest('[draggable="true"]')!;
    const setData = vi.fn();
    fireEvent.dragStart(item, { dataTransfer: { setData } });
    expect(setData).toHaveBeenCalledWith('application/hermes-node-type', 'llm');
  });
});
