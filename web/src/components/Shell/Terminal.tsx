import { useEffect, useRef } from 'react';
import { Terminal as XTerminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import apiClient from '../../api/client';
import wsManager from '../../api/ws';
import { GenerateID } from '../../api/helpers';

interface Props {
  deviceId: string;
}

export default function Terminal({ deviceId }: Props) {
  const termRef = useRef<HTMLDivElement>(null);
  const termInstance = useRef<XTerminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const sessionId = useRef<string>('');
  const wsHandlerRef = useRef<((env: any) => void) | null>(null);

  useEffect(() => {
    if (!termRef.current) return;

    const term = new XTerminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
      },
    });

    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(termRef.current);
    fit.fit();

    termInstance.current = term;
    fitAddon.current = fit;

    // Start shell session
    const sid = GenerateID();
    sessionId.current = sid;

    const startShell = async () => {
      try {
        await apiClient.post(`/devices/${deviceId}/shell`, {
          session_id: sid,
          cols: term.cols,
          rows: term.rows,
        });

      } catch (e: any) {
        term.writeln('\x1b[31m--- Failed to start shell ---\x1b[0m\r');
      }
    };

    startShell();

    // Handle user input
    term.onData((data) => {
      const env = {
        id: GenerateID(),
        channel: 'shell',
        type: 'request',
        action: 'shell.input',
        payload: {
          device_id: deviceId,
          session_id: sid,
          data: btoa(data),
        },
        ts: Date.now(),
      };
      wsManager.send(env);
    });

    // Handle resize
    term.onResize(({ cols, rows }) => {
      const env = {
        id: GenerateID(),
        channel: 'shell',
        type: 'request',
        action: 'shell.resize',
        payload: { device_id: deviceId, session_id: sid, cols, rows },
        ts: Date.now(),
      };
      wsManager.send(env);
    });

    // Listen for shell output
    const handler = (env: any) => {
      if (env.action === 'shell.output' && env.payload?.session_id === sid) {
        try {
          const decoded = atob(env.payload.data);
          term.write(decoded);
        } catch { /* ignore */ }
      }
    };
    wsManager.on('shell.output', handler);
    wsHandlerRef.current = handler;

    // Handle window resize
    const handleResize = () => fit.fit();
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
      wsManager.off('shell.output', handler);

      // Close shell session
      wsManager.send({
        id: GenerateID(),
        channel: 'shell',
        type: 'request',
        action: 'shell.close',
        payload: { device_id: deviceId, session_id: sid },
        ts: Date.now(),
      });

      term.dispose();
    };
  }, [deviceId]);

  return <div ref={termRef} style={{ height: 'calc(100vh - 280px)', minHeight: 400 }} />;
}
