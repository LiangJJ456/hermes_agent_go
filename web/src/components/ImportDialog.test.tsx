import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ImportDialog } from './ImportDialog';

describe('ImportDialog', () => {
  it('parses valid JSON and calls onImport with the object', () => {
    const onImport = vi.fn();
    render(<ImportDialog onImport={onImport} onClose={() => {}} />);
    fireEvent.change(screen.getByLabelText('Graph JSON'), {
      target: { value: '{"StartAt":"a","Nodes":{},"Edges":[]}' },
    });
    fireEvent.click(screen.getByText('Import'));
    expect(onImport).toHaveBeenCalledWith({ StartAt: 'a', Nodes: {}, Edges: [] });
  });

  it('shows an error and does not call onImport for invalid JSON', () => {
    const onImport = vi.fn();
    render(<ImportDialog onImport={onImport} onClose={() => {}} />);
    fireEvent.change(screen.getByLabelText('Graph JSON'), { target: { value: '{nope' } });
    fireEvent.click(screen.getByText('Import'));
    expect(onImport).not.toHaveBeenCalled();
    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
  });
});
