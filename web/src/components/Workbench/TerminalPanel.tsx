import { useState, useRef, useEffect, useCallback } from 'react';
import { Tooltip, message } from 'antd';
import { CodeOutlined, CopyOutlined } from '@ant-design/icons';
import apiClient from '../../api/client';
import wsManager from '../../api/ws';
import { GenerateID } from '../../api/helpers';
import ScriptLibrary from './ScriptLibrary';
import type { TerminalTab } from './types';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';

interface Props {
  deviceId: string;
}

export default function TerminalPanel({ deviceId }: Props) {
  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string>('');
  const [scriptPanelVisible, setScriptPanelVisible] = useState(false);
  const tabsRef = useRef<TerminalTab[]>([]);
  const activeTabIdRef = useRef<string>('');
  const terminalsRef = useRef<Map<string, { term: Terminal; fit: FitAddon; disposable?: { dispose: () => void } }>>(new Map());
  const termContainerRef = useRef<Map<string, HTMLDivElement>>(new Map());
  const resizeObserverRef = useRef<ResizeObserver | null>(null);

// Shared terminal configuration
const TERMINAL_OPTIONS: ConstructorParameters<typeof Terminal>[0] = {
  cursorBlink: true,
  cursorStyle: 'block',
  fontSize: 14,
  fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', 'Courier New', monospace",
  theme: {
    background: '#1e1e1e',
    foreground: '#d4d4d4',
    cursor: '#d4d4d4',
    selectionBackground: '#264f78',
    black: '#000000',
    red: '#cd3131',
    green: '#0dbc79',
    yellow: '#e5e510',
    blue: '#2472c8',
    magenta: '#bc3fbc',
    cyan: '#11a8cd',
    white: '#e5e5e5',
    brightBlack: '#666666',
    brightRed: '#f14c4c',
    brightGreen: '#23d18b',
    brightYellow: '#f5f543',
    brightBlue: '#3b8eea',
    brightMagenta: '#d670d6',
    brightCyan: '#29b8db',
    brightWhite: '#ffffff',
  },
  scrollback: 5000,
  convertEol: false,
};

  // Keep refs in sync
  useEffect(() => { tabsRef.current = tabs; }, [tabs]);
  useEffect(() => { activeTabIdRef.current = activeTabId; }, [activeTabId]);

  // Send raw data to shell via WebSocket
  const sendToShell = useCallback((sessionId: string, data: string) => {
    wsManager.send({
      id: GenerateID(),
      channel: 'shell',
      type: 'request' as const,
      action: 'shell.input',
      payload: { device_id: deviceId, session_id: sessionId, data: btoa(data) },
      ts: Date.now(),
    });
  }, [deviceId]);

  // Start shell session and wire up terminal
  const startShellSession = useCallback(async (tabId: string, sessionId: string, term: Terminal, fit: FitAddon) => {
    try {
      const cols = term.cols || 80;
      const rows = term.rows || 24;
      const { data } = await apiClient.post(`/devices/${deviceId}/shell`, {
        session_id: sessionId,
        cols,
        rows,
      });
      const actualSessionId = data?.session_id || sessionId;
      if (actualSessionId !== sessionId) {
        setTabs(prev => {
          const next = prev.map(t => t.id === tabId ? { ...t, sessionId: actualSessionId } : t);
          tabsRef.current = next;
          return next;
        });
      }

      // Wire up terminal input → shell
      const inputDisposable = term.onData((data: string) => {
        sendToShell(actualSessionId, data);
      });

      // Wire up terminal resize → shell resize
      const resizeDisposable = term.onResize(({ cols: c, rows: r }) => {
        wsManager.send({
          id: GenerateID(),
          channel: 'shell',
          type: 'request' as const,
          action: 'shell.resize',
          payload: { device_id: deviceId, session_id: actualSessionId, cols: c, rows: r },
          ts: Date.now(),
        });
      });

      // Store disposables for cleanup
      const entry = terminalsRef.current.get(tabId);
      if (entry) {
        entry.disposable = {
          dispose: () => {
            inputDisposable.dispose();
            resizeDisposable.dispose();
          },
        };
      }

      term.focus();

      // Do initial fit after mount
      setTimeout(() => {
        try { fit.fit(); } catch { /* ignore */ }
      }, 100);
    } catch {
      term.writeln('\x1b[31m--- Failed to start shell ---\x1b[0m');
    }
  }, [deviceId, sendToShell]);

  const createNewTab = useCallback(() => {
    const id = GenerateID();
    const sessionId = GenerateID();
    const newTab: TerminalTab = {
      id,
      title: `Terminal ${tabsRef.current.length + 1}`,
      sessionId,
    };
    setTabs(prev => {
      const next = [...prev, newTab];
      tabsRef.current = next;
      return next;
    });
    setActiveTabId(id);
    activeTabIdRef.current = id;

    // Terminal creation and session start will happen in useEffect when DOM is ready
  }, []);

  const closeTab = useCallback((tabId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setTabs(prev => {
      const tab = prev.find(t => t.id === tabId);
      if (tab) {
        wsManager.send({
          id: GenerateID(),
          channel: 'shell',
          type: 'request' as const,
          action: 'shell.close',
          payload: { device_id: deviceId, session_id: tab.sessionId },
          ts: Date.now(),
        });
      }
      const remaining = prev.filter(t => t.id !== tabId);
      if (activeTabIdRef.current === tabId && remaining.length > 0) {
        setActiveTabId(remaining[remaining.length - 1].id);
      }
      if (remaining.length === 0) {
        setActiveTabId('');
      }
      return remaining;
    });
    // Cleanup terminal instance
    const entry = terminalsRef.current.get(tabId);
    if (entry) {
      entry.disposable?.dispose();
      entry.term.dispose();
      terminalsRef.current.delete(tabId);
    }
    termContainerRef.current.delete(tabId);
  }, [deviceId]);

  // Register WS handler for shell.output
  useEffect(() => {
    const handler = (env: any) => {
      if (env.action !== 'shell.output') return;
      const sid = env.payload?.session_id;
      if (!sid) return;
      try {
        const b64 = env.payload.data;
        // Decode base64 → Uint8Array → UTF-8 string
        const binary = atob(b64);
        const bytes = new Uint8Array(binary.length);
        for (let k = 0; k < binary.length; k++) bytes[k] = binary.charCodeAt(k);
        const decoded = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
        // Find the tab with this session and write to its terminal
        const tab = tabsRef.current.find(t => t.sessionId === sid);
        if (tab) {
          const entry = terminalsRef.current.get(tab.id);
          if (entry) {
            entry.term.write(decoded);
          }
        }
      } catch { /* ignore */ }
    };
    wsManager.on('shell.output', handler);
    return () => { wsManager.off('shell.output', handler); };
  }, []);

  // Auto-create first tab
  useEffect(() => {
    if (tabsRef.current.length === 0) {
      createNewTab();
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // When activeTabId changes, show the correct terminal container and fit
  useEffect(() => {
    // Hide all containers, show active one
    termContainerRef.current.forEach((container, tabId) => {
      container.style.display = tabId === activeTabId ? 'block' : 'none';
    });
    // Fit the active terminal
    if (activeTabId) {
      const entry = terminalsRef.current.get(activeTabId);
      if (entry) {
        setTimeout(() => {
          try { entry.fit.fit(); } catch { /* ignore */ }
          entry.term.focus();
        }, 50);
      }
    }
  }, [activeTabId]);

  // Handle new tabs: when a tab is added and its container div appears, create terminal and start session
  useEffect(() => {
    tabs.forEach(tab => {
      if (terminalsRef.current.has(tab.id)) return; // Already has a terminal
      const container = termContainerRef.current.get(tab.id);
      if (!container) return; // DOM not ready yet

      const term = new Terminal(TERMINAL_OPTIONS);
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(container);
      try { fit.fit(); } catch { /* ignore */ }

      terminalsRef.current.set(tab.id, { term, fit });
      startShellSession(tab.id, tab.sessionId, term, fit);
    });
  }, [tabs, startShellSession]);

  // ResizeObserver for auto-fit on container resize
  useEffect(() => {
    const container = document.querySelector('[data-term-output-area]');
    if (!container) return;
    const observer = new ResizeObserver(() => {
      const entry = terminalsRef.current.get(activeTabIdRef.current);
      if (entry) {
        try { entry.fit.fit(); } catch { /* ignore */ }
      }
    });
    observer.observe(container);
    resizeObserverRef.current = observer;
    return () => { observer.disconnect(); };
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      // Send shell.close for each active session
      tabsRef.current.forEach(tab => {
        wsManager.send({
          id: GenerateID(),
          channel: 'shell',
          type: 'request' as const,
          action: 'shell.close',
          payload: { device_id: deviceId, session_id: tab.sessionId },
          ts: Date.now(),
        });
      });
      terminalsRef.current.forEach(entry => {
        entry.disposable?.dispose();
        entry.term.dispose();
      });
      terminalsRef.current.clear();
    };
  }, [deviceId]);

  const runScript = useCallback((command: string) => {
    const tab = tabsRef.current.find(t => t.id === activeTabIdRef.current);
    if (!tab) return;
    sendToShell(tab.sessionId, command + '\r');
    const entry = terminalsRef.current.get(tab.id);
    if (entry) entry.term.focus();
  }, [sendToShell]);

  // Container ref callback — stores the DOM element for each tab
  const setContainerRef = useCallback((tabId: string, el: HTMLDivElement | null) => {
    if (el) {
      termContainerRef.current.set(tabId, el);
    } else {
      termContainerRef.current.delete(tabId);
    }
  }, []);

  return (
    <div style={{ height: '100%', display: 'flex', background: '#1e1e1e' }}>
      {/* Script library sidebar */}
      {scriptPanelVisible && (
        <div style={{ width: 240, flexShrink: 0, borderRight: '1px solid #3c3c3c', overflow: 'hidden' }}>
          <ScriptLibrary onRun={runScript} />
        </div>
      )}

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        {/* Tab bar */}
        <div style={{ display: 'flex', alignItems: 'stretch', background: '#252526', borderBottom: '1px solid #3c3c3c', minHeight: 35, flexShrink: 0 }}>
          {tabs.map(tab => (
            <div
              key={tab.id}
              onClick={() => setActiveTabId(tab.id)}
              style={{
                display: 'flex', alignItems: 'center', padding: '0 12px', cursor: 'pointer',
                fontSize: 12, color: tab.id === activeTabId ? '#ffffff' : '#969696',
                background: tab.id === activeTabId ? '#1e1e1e' : '#2d2d2d',
                borderRight: '1px solid #3c3c3c', whiteSpace: 'nowrap', gap: 6,
                borderTop: tab.id === activeTabId ? '2px solid #007acc' : '2px solid transparent',
              }}
            >
              <span>{tab.title}</span>
              <span
                onClick={(e) => closeTab(tab.id, e)}
                style={{ marginLeft: 4, opacity: 0.6, fontSize: 14, lineHeight: 1 }}
              >&times;</span>
            </div>
          ))}
          <div
            onClick={() => createNewTab()}
            style={{ display: 'flex', alignItems: 'center', padding: '0 10px', cursor: 'pointer', color: '#cccccc', fontSize: 18, flexShrink: 0 }}
            title="新建终端"
          >+</div>
          <div style={{ flex: 1 }} />
          <Tooltip title="复制全部输出">
            <div
              onClick={() => {
                const entry = terminalsRef.current.get(activeTabIdRef.current);
                if (entry) {
                  const text = entry.term.buffer.active.toString();
                  navigator.clipboard.writeText(text).then(() => message.success('已复制', 0.5));
                }
              }}
              style={{ display: 'flex', alignItems: 'center', padding: '0 10px', cursor: 'pointer', color: '#969696', fontSize: 14, flexShrink: 0 }}
            ><CopyOutlined /></div>
          </Tooltip>
          <Tooltip title={scriptPanelVisible ? '关闭脚本库' : '脚本库'}>
            <div
              onClick={() => setScriptPanelVisible(!scriptPanelVisible)}
              style={{
                display: 'flex', alignItems: 'center', padding: '0 10px', cursor: 'pointer',
                color: scriptPanelVisible ? '#007acc' : '#969696', fontSize: 14, flexShrink: 0,
              }}
            ><CodeOutlined /></div>
          </Tooltip>
        </div>

        {/* Terminal containers — one per tab, hidden/shown based on active */}
        <div data-term-output-area style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
          {tabs.map(tab => (
            <div
              key={tab.id}
              ref={(el) => setContainerRef(tab.id, el)}
              style={{
                position: 'absolute',
                top: 0, left: 0, right: 0, bottom: 0,
                display: tab.id === activeTabId ? 'block' : 'none',
                padding: '4px 0 0 8px',
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
