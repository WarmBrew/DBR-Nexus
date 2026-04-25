import { useCallback } from 'react';
import type { OpenFile } from './types';

interface Props {
  files: OpenFile[];
  activeIndex: number;
  onSwitch: (index: number) => void;
  onClose: (index: number) => void;
  onNew: () => void;
  onContentChange: (index: number, content: string) => void;
  onSave: (index: number) => void;
  onLineEndingChange: (index: number, le: 'LF' | 'CRLF') => void;
}

export default function EditorPanel({
  files, activeIndex, onSwitch, onClose, onNew, onContentChange, onSave, onLineEndingChange,
}: Props) {
  const activeFile = files[activeIndex] || null;

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.ctrlKey && e.key === 's') {
      e.preventDefault();
      if (activeIndex >= 0) onSave(activeIndex);
    }
    // Tab key inserts spaces
    if (e.key === 'Tab') {
      e.preventDefault();
      const ta = e.currentTarget;
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      const val = ta.value;
      const newVal = val.substring(0, start) + '  ' + val.substring(end);
      onContentChange(activeIndex, newVal);
      requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = start + 2; });
    }
  }, [activeIndex, onSave, onContentChange]);

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: '#1e1e1e' }}>
      {/* Tab bar */}
      <div style={{ display: 'flex', alignItems: 'stretch', background: '#252526', borderBottom: '1px solid #3c3c3c', minHeight: 35, flexShrink: 0, overflow: 'hidden' }}>
        <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
          {files.map((f, i) => {
            const isActive = i === activeIndex;
            const isDirty = f.content !== f.originalContent;
            return (
              <div
                key={i}
                onClick={() => onSwitch(i)}
                style={{
                  display: 'flex', alignItems: 'center', padding: '0 12px', cursor: 'pointer',
                  fontSize: 13, color: isActive ? '#ffffff' : '#969696',
                  background: isActive ? '#1e1e1e' : '#2d2d2d',
                  borderRight: '1px solid #3c3c3c', whiteSpace: 'nowrap', gap: 6, flexShrink: 0,
                  borderTop: isActive ? '2px solid #007acc' : '2px solid transparent',
                }}
              >
                {isDirty && <span style={{ color: '#e5e510', fontSize: 10 }}>&#9679;</span>}
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{f.name || 'untitled'}</span>
                <span
                  onClick={(e) => { e.stopPropagation(); onClose(i); }}
                  style={{ marginLeft: 4, opacity: 0.6, fontSize: 14, lineHeight: 1 }}
                  title="关闭"
                >
                  &times;
                </span>
              </div>
            );
          })}
        </div>
        <div
          onClick={onNew}
          style={{ display: 'flex', alignItems: 'center', padding: '0 10px', cursor: 'pointer', color: '#cccccc', fontSize: 18, flexShrink: 0 }}
          title="新建标签"
        >
          +
        </div>
      </div>

      {/* Toolbar */}
      {activeFile && (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '4px 12px', background: '#252526', borderBottom: '1px solid #3c3c3c', flexShrink: 0 }}>
          <button
            onClick={() => onSave(activeIndex)}
            style={{
              background: '#0e639c', color: '#fff', border: 'none', borderRadius: 3,
              padding: '2px 12px', fontSize: 12, cursor: 'pointer',
            }}
          >
            保存 (Ctrl+S)
          </button>
          <div style={{ display: 'flex', gap: 0, background: '#3c3c3c', borderRadius: 10, overflow: 'hidden', fontSize: 11 }}>
            <div
              onClick={() => onLineEndingChange(activeIndex, 'LF')}
              style={{ padding: '2px 10px', cursor: 'pointer', color: activeFile.lineEnding === 'LF' ? '#fff' : '#888', background: activeFile.lineEnding === 'LF' ? '#0e639c' : 'transparent' }}
            >LF</div>
            <div
              onClick={() => onLineEndingChange(activeIndex, 'CRLF')}
              style={{ padding: '2px 10px', cursor: 'pointer', color: activeFile.lineEnding === 'CRLF' ? '#fff' : '#888', background: activeFile.lineEnding === 'CRLF' ? '#0e639c' : 'transparent' }}
            >CRLF</div>
          </div>
        </div>
      )}

      {/* Editor area */}
      <div style={{ flex: 1, overflow: 'hidden', position: 'relative' }}>
        {activeFile ? (
          <textarea
            value={activeFile.content}
            onChange={(e) => onContentChange(activeIndex, e.target.value)}
            onKeyDown={handleKeyDown}
            spellCheck={false}
            style={{
              width: '100%', height: '100%', resize: 'none', border: 'none', outline: 'none',
              background: '#1e1e1e', color: '#d4d4d4', padding: '8px 16px',
              fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
              fontSize: 14, lineHeight: 1.6, tabSize: 2, whiteSpace: 'pre',
              overflow: 'auto',
            }}
          />
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#555555', fontSize: 14 }}>
            点击左侧文件树打开文件，或按 + 新建标签
          </div>
        )}
      </div>
    </div>
  );
}
