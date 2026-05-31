import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { NodeCard, summarize } from './NodeCard';
import type { NodeData } from '../model/types';

function renderNode(data: NodeData, hasError = false) {
  return render(
    <ReactFlowProvider>
      <NodeCard
        id="classify"
        data={{ ...data, _hasError: hasError }}
        selected={false}
        type="hermes"
        dragging={false}
        draggable={false}
        selectable={false}
        deletable={false}
        zIndex={0}
        isConnectable
        positionAbsoluteX={0}
        positionAbsoluteY={0}
      />
    </ReactFlowProvider>,
  );
}

describe('NodeCard', () => {
  it('shows id, type, and a key-field summary', () => {
    renderNode({ nodeType: 'llm', config: { Model: 'deepseek-v4' }, isStart: true });
    expect(screen.getByText('classify')).toBeInTheDocument();
    expect(screen.getByText('llm')).toBeInTheDocument();
    expect(screen.getByText(/deepseek-v4/)).toBeInTheDocument();
  });

  it('shows an error badge when flagged', () => {
    renderNode({ nodeType: 'tool', config: {}, isStart: false }, true);
    expect(screen.getByLabelText('has errors')).toBeInTheDocument();
  });
});

describe('summarize', () => {
  it('picks the key field per type', () => {
    expect(summarize('llm', { Model: 'm' })).toBe('m');
    expect(summarize('tool', { Resource: 'r' })).toBe('r');
    expect(summarize('end', { Status: 's' })).toBe('s');
    expect(summarize('choice', { Choices: [1, 2] })).toBe('2 branches');
  });
});
