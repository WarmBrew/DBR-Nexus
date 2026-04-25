import { useState, useCallback, useRef, useEffect } from 'react';
import { message } from 'antd';
import apiClient from '../../api/client';
import FileTreePanel from './FileTreePanel';
import EditorPanel from './EditorPanel';
import TerminalPanel from './TerminalPanel';
import type { OpenFile, PanelState } from './types';

interface Props {
  deviceId: string;
}

const DEFAULT_PANELS: Record<string, PanelState> = {
  fileTree: { visible: true, width: 18 },
  editor: { visible: true, width: 46 },
  terminal: { visible: true, width: 36 },
};

function detectLanguage(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() || '';
  const map: Record<string, string> = {
    go: 'go', py: 'python', js: 'javascript', ts: 'typescript',
    jsx: 'javascript', tsx: 'typescript', rs: 'rust', java: 'java',
    c: 'c', cpp: 'cpp', h: 'c', yaml: 'yaml', yml: 'yaml',
    json: 'json', xml: 'xml', html: 'html', css: 'css',
    sh: 'shell', bash: 'shell', sql: 'sql', md: 'markdown',
    toml: 'toml', ini: 'ini',
  };
  return map[ext] || 'plaintext';
}

export default function Workbench({ deviceId }: Props) {
  const [panels, setPanels] = useState<Record<string, PanelState>>({ ...DEFAULT_PANELS });
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([]);
  const [activeFileIndex, setActiveFileIndex] = useState(-1);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ side: 'left' | 'right'; lastX: number } | null>(null);

  // Toggle panel visibility with proportional fill
  const togglePanel = useCallback((key: string) => {
    setPanels(prev => {
      const next = { ...prev };
      const wasVisible = next[key].visible;
      const closedWidth = next[key].width;

      next[key] = { ...next[key], visible: !wasVisible };

      const visibleKeys = Object.keys(next).filter(k => next[k].visible);

      if (!wasVisible) {
        // Opening: give it a default share, shrink others
        const totalCurrent = visibleKeys.filter(k => k !== key).reduce((s, k) => s + next[k].width, 0);
        const newWidth = key === 'fileTree' ? 18 : key === 'terminal' ? 36 : 46;
        const scale = (100 - newWidth) / totalCurrent;
        for (const k of visibleKeys) {
          if (k !== key) next[k] = { ...next[k], width: next[k].width * scale };
        }
        next[key].width = newWidth;
      } else {
        // Closing: redistribute width to remaining panels
        const totalRemaining = visibleKeys.reduce((s, k) => s + next[k].width, 0);
        for (const k of visibleKeys) {
          next[k] = { ...next[k], width: (next[k].width / totalRemaining) * 100 };
        }
        next[key].width = closedWidth;
      }
      return next;
    });
  }, []);

  // Open file from tree
  const handleOpenFile = useCallback(async (path: string, name: string) => {
    // Check if already open (use functional update to get latest state)
    let existingIndex = -1;
    setOpenFiles(prev => {
      existingIndex = prev.findIndex(f => f.path === path);
      return prev; // no change, just reading
    });

    if (existingIndex >= 0) {
      setActiveFileIndex(existingIndex);
      setSelectedPath(path);
      return;
    }

    // Fetch content
    try {
      const { data } = await apiClient.get(`/devices/${deviceId}/files/content`, { params: { path } });
      const content = data.content || '';
      const lineEnding = content.includes('\r\n') ? 'CRLF' as const : 'LF' as const;
      const file: OpenFile = {
        path: data.path || path,
        name,
        content: content.replace(/\r\n/g, '\n'),
        originalContent: content.replace(/\r\n/g, '\n'),
        language: data.language || detectLanguage(path),
        lineEnding,
      };
      setOpenFiles(prev => {
        const newIndex = prev.length; // this is correct within functional update
        // Use setTimeout to avoid setState during render
        setTimeout(() => {
          setActiveFileIndex(newIndex);
          setSelectedPath(path);
        }, 0);
        return [...prev, file];
      });
    } catch (e: any) {
      const errMsg = e.response?.data?.error || '读取文件失败';
      message.error(errMsg);
    }
  }, [deviceId]);

  // Editor callbacks
  const handleContentChange = useCallback((index: number, content: string) => {
    setOpenFiles(prev => prev.map((f, i) => i === index ? { ...f, content } : f));
  }, []);

  const handleSave = useCallback(async (index: number) => {
    let fileToSave: OpenFile | null = null;
    setOpenFiles(prev => {
      fileToSave = prev[index] || null;
      return prev;
    });
    if (!fileToSave) return;
    const file = fileToSave;
    const le = file.lineEnding === 'CRLF' ? '\r\n' : '\n';
    const content = file.content.replace(/\n/g, le);
    try {
      await apiClient.put(`/devices/${deviceId}/files/content`, { path: file.path, content });
      setOpenFiles(prev => prev.map((f, i) => i === index ? { ...f, originalContent: f.content } : f));
    } catch (e: any) {
      message.error(e.response?.data?.error || '保存文件失败');
    }
  }, [deviceId]);

  const handleLineEndingChange = useCallback((index: number, le: 'LF' | 'CRLF') => {
    setOpenFiles(prev => prev.map((f, i) => i === index ? { ...f, lineEnding: le } : f));
  }, []);

  const handleNewTab = useCallback(() => {
    const file: OpenFile = {
      path: '', name: 'untitled', content: '', originalContent: '',
      language: 'plaintext', lineEnding: 'LF',
    };
    setOpenFiles(prev => {
      const newIndex = prev.length;
      setTimeout(() => setActiveFileIndex(newIndex), 0);
      return [...prev, file];
    });
  }, []);

  const handleCloseTab = useCallback((index: number) => {
    setOpenFiles(prev => prev.filter((_, i) => i !== index));
    setActiveFileIndex(prev => {
      const newLen = prev - 1; // approximate
      if (index < prev) return Math.max(0, prev - 1);
      if (index === prev) return Math.max(0, prev - 1);
      return prev;
    });
  }, []);

  // Drag resize logic
  const handleMouseDown = useCallback((side: 'left' | 'right') => (e: React.MouseEvent) => {
    e.preventDefault();
    dragRef.current = { side, lastX: e.clientX };
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, []);

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!dragRef.current || !containerRef.current) return;
      const { side, lastX } = dragRef.current;
      const totalWidth = containerRef.current.offsetWidth;
      const dx = e.clientX - lastX;
      const dPercent = (dx / totalWidth) * 100;
      dragRef.current.lastX = e.clientX;

      setPanels(prev => {
        const next = { ...prev };
        const visibleKeys = Object.keys(next).filter(k => next[k].visible);

        if (visibleKeys.length < 2) return prev;

        let keyA: string, keyB: string;
        if (side === 'left') {
          // Drag between first and second visible panels
          keyA = visibleKeys[0];
          keyB = visibleKeys[1];
        } else {
          // Drag between last two visible panels (or second-to-last and last)
          const idx = visibleKeys.length >= 3 ? 1 : 0;
          keyA = visibleKeys[idx];
          keyB = visibleKeys[idx + 1];
        }

        const combined = prev[keyA].width + prev[keyB].width;
        const newWA = Math.max(8, Math.min(combined - 8, prev[keyA].width + dPercent));
        const newWB = combined - newWA;
        next[keyA] = { ...prev[keyA], width: newWA };
        next[keyB] = { ...prev[keyB], width: newWB };
        return next;
      });
    };

    const handleMouseUp = () => {
      dragRef.current = null;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
    return () => {
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };
  }, []);

  // Build visible panel layout
  const visibleKeys = Object.keys(panels).filter(k => panels[k].visible);

  const renderResizeHandle = (side: 'left' | 'right') => (
    <div
      onMouseDown={handleMouseDown(side)}
      style={{
        width: 4, cursor: 'col-resize', background: '#3c3c3c',
        transition: 'background 0.15s', flexShrink: 0,
      }}
      onMouseEnter={e => (e.currentTarget.style.background = '#007acc')}
      onMouseLeave={e => (e.currentTarget.style.background = '#3c3c3c')}
    />
  );

  return (
    <div ref={containerRef} style={{ height: '100%', display: 'flex', flexDirection: 'column', background: '#1e1e1e' }}>
      {/* Toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0 8px', background: '#333333', borderBottom: '1px solid #3c3c3c', minHeight: 32, flexShrink: 0 }}>
        <div style={{ display: 'flex', gap: 4 }}>
          <button
            onClick={() => togglePanel('fileTree')}
            style={{
              background: panels.fileTree.visible ? '#0e639c' : '#3c3c3c', color: '#fff', border: 'none',
              borderRadius: 3, padding: '2px 10px', fontSize: 11, cursor: 'pointer',
            }}
          >
            文件树
          </button>
          <button
            onClick={() => togglePanel('editor')}
            style={{
              background: panels.editor.visible ? '#0e639c' : '#3c3c3c', color: '#fff', border: 'none',
              borderRadius: 3, padding: '2px 10px', fontSize: 11, cursor: 'pointer',
            }}
          >
            编辑器
          </button>
          <button
            onClick={() => togglePanel('terminal')}
            style={{
              background: panels.terminal.visible ? '#0e639c' : '#3c3c3c', color: '#fff', border: 'none',
              borderRadius: 3, padding: '2px 10px', fontSize: 11, cursor: 'pointer',
            }}
          >
            终端
          </button>
        </div>
      </div>

      {/* Main content */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* File Tree */}
        {panels.fileTree.visible && (
          <>
            <div style={{ width: `${panels.fileTree.width}%`, minWidth: 120, overflow: 'hidden' }}>
              <FileTreePanel deviceId={deviceId} onOpenFile={handleOpenFile} selectedPath={selectedPath} />
            </div>
            {visibleKeys.indexOf('fileTree') < visibleKeys.length - 1 && renderResizeHandle('left')}
          </>
        )}

        {/* Editor */}
        {panels.editor.visible && (
          <>
            <div style={{ width: `${panels.editor.width}%`, minWidth: 200, overflow: 'hidden' }}>
              <EditorPanel
                files={openFiles}
                activeIndex={activeFileIndex}
                onSwitch={setActiveFileIndex}
                onClose={handleCloseTab}
                onNew={handleNewTab}
                onContentChange={handleContentChange}
                onSave={handleSave}
                onLineEndingChange={handleLineEndingChange}
              />
            </div>
            {visibleKeys.indexOf('editor') < visibleKeys.length - 1 && renderResizeHandle('right')}
          </>
        )}

        {/* Terminal */}
        {panels.terminal.visible && (
          <div style={{ width: `${panels.terminal.width}%`, minWidth: 200, overflow: 'hidden' }}>
            <TerminalPanel deviceId={deviceId} />
          </div>
        )}
      </div>
    </div>
  );
}
