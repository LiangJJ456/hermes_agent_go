import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AiPanel } from './AiPanel';

describe('AiPanel', () => {
  it('submits the typed instruction', () => {
    const onSubmit = vi.fn();
    render(<AiPanel onSubmit={onSubmit} busy={false} error={null} notes={null} canUndo={false} onUndo={() => {}} />);
    fireEvent.change(screen.getByLabelText('AI instruction'), { target: { value: '做一个分类流程' } });
    fireEvent.click(screen.getByText('生成'));
    expect(onSubmit).toHaveBeenCalledWith('做一个分类流程');
  });

  it('disables the button and shows busy state', () => {
    render(<AiPanel onSubmit={() => {}} busy={true} error={null} notes={null} canUndo={false} onUndo={() => {}} />);
    expect(screen.getByText('生成中…').closest('button')).toBeDisabled();
  });

  it('shows an error message', () => {
    render(<AiPanel onSubmit={() => {}} busy={false} error="未配置模型" notes={null} canUndo={false} onUndo={() => {}} />);
    expect(screen.getByText('未配置模型')).toBeInTheDocument();
  });

  it('shows notes and undo, and calls onUndo', () => {
    const onUndo = vi.fn();
    render(<AiPanel onSubmit={() => {}} busy={false} error={null} notes="已生成 3 个节点" canUndo={true} onUndo={onUndo} />);
    expect(screen.getByText('已生成 3 个节点')).toBeInTheDocument();
    fireEvent.click(screen.getByText('撤销'));
    expect(onUndo).toHaveBeenCalled();
  });
});
