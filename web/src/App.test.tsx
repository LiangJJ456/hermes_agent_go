import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import App from './App';

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/nodetypes') {
        return { ok: true, status: 200, json: async () => [{ type: 'llm', fields: [] }, { type: 'end', fields: [] }] };
      }
      return { ok: true, status: 200, json: async () => ({ valid: true, errors: [] }) };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe('App', () => {
  it('loads node types into the palette and shows the toolbar', async () => {
    render(<App />);
    expect(screen.getByText('Import')).toBeInTheDocument();
    expect(screen.getByText('Export')).toBeInTheDocument();
    expect(screen.getByText('Validate')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('llm')).toBeInTheDocument());
  });

  it('runs validation and shows the result', async () => {
    render(<App />);
    await waitFor(() => screen.getByText('llm'));
    fireEvent.click(screen.getByText('Validate'));
    await waitFor(() => expect(screen.getByText(/no errors/i)).toBeInTheDocument());
  });
});
