import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ErrorPanel } from './ErrorPanel';

const errors = [
  { path: 'edges[0].to', message: 'edge 0: To references unknown node "ghost"' },
  { path: '<graph>', message: 'edge 0 (a->b): bad condition' },
];

describe('ErrorPanel', () => {
  it('lists every error with path and message', () => {
    render(<ErrorPanel errors={errors} onFocus={() => {}} />);
    expect(screen.getByText(/unknown node "ghost"/)).toBeInTheDocument();
    expect(screen.getByText(/bad condition/)).toBeInTheDocument();
    expect(screen.getByText('edges[0].to')).toBeInTheDocument();
  });

  it('focuses the mapped element when a row is clicked', () => {
    const onFocus = vi.fn();
    render(<ErrorPanel errors={errors} onFocus={onFocus} />);
    fireEvent.click(screen.getByText(/unknown node "ghost"/));
    expect(onFocus).toHaveBeenCalledWith({ kind: 'edge', index: 0 });
  });

  it('renders nothing visible-ish when no errors', () => {
    render(<ErrorPanel errors={[]} onFocus={() => {}} />);
    expect(screen.getByText(/no errors/i)).toBeInTheDocument();
  });
});
