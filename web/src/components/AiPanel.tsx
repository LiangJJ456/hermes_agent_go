import { useState } from 'react';

interface Props {
  onSubmit: (instruction: string) => void;
  busy: boolean;
  error: string | null;
  notes: string | null;
  canUndo: boolean;
  onUndo: () => void;
}

export function AiPanel({ onSubmit, busy, error, notes, canUndo, onUndo }: Props) {
  const [text, setText] = useState('');

  function submit() {
    const t = text.trim();
    if (t && !busy) onSubmit(t);
  }

  return (
    <div className="aipanel">
      <div className="aipanel-title">AI 生成</div>
      <textarea
        aria-label="AI instruction"
        className="aipanel-input"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) submit();
        }}
        rows={3}
        placeholder="描述你想要的工作流,例如:先用 LLM 分类,再并行调用搜索和计算两个工具,最后汇总输出"
      />
      <div className="aipanel-actions">
        <button type="button" onClick={submit} disabled={busy}>
          {busy ? '生成中…' : '生成'}
        </button>
        {canUndo && (
          <button type="button" onClick={onUndo}>
            撤销
          </button>
        )}
      </div>
      {error && <div className="aipanel-error">{error}</div>}
      {notes && <div className="aipanel-notes">{notes}</div>}
    </div>
  );
}
