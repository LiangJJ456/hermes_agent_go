import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { Canvas } from './Canvas';
import type { EditorNode, EditorEdge } from '../model/graph';

const nodes: EditorNode[] = [
  { id: 'a', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'llm', config: { Model: 'm' }, isStart: true } },
];
const edges: EditorEdge[] = [];

describe('Canvas', () => {
  it('renders a node from props', () => {
    render(
      <ReactFlowProvider>
        <Canvas
          nodes={nodes}
          edges={edges}
          onNodesChange={() => {}}
          onEdgesChange={() => {}}
          onConnect={() => {}}
          onDropNode={() => {}}
          onSelect={() => {}}
        />
      </ReactFlowProvider>,
    );
    expect(screen.getByText('a')).toBeInTheDocument();
  });
});
