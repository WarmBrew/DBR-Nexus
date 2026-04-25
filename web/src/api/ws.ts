import type { Envelope } from './types';
import { CryptoSession, checkSecureContext } from './crypto';

type MessageHandler = (env: Envelope) => void;

class WebSocketManager {
  private conn: WebSocket | null = null;
  private pending: Map<string, { resolve: (env: Envelope) => void; reject: (err: Error) => void; timer: ReturnType<typeof setTimeout> }> = new Map();
  private handlers: Map<string, MessageHandler[]> = new Map();
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 10;
  private token: string = '';
  private onConnectionChange?: (connected: boolean) => void;
  private latency: number = -1;
  private latencyCallback?: (ms: number) => void;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private cryptoSession: CryptoSession | null = null;
  private dummyTimer: ReturnType<typeof setTimeout> | null = null;
  private encrypted: boolean = false;
  private sendQueue: Envelope[] = [];
  private sendChain: Promise<void> = Promise.resolve();
  private sending: boolean = false;

  connect(token: string) {
    // Close existing connection if any
    if (this.conn) {
      this.stopPing();
      this.stopDummy();
      this.conn.onclose = null;
      this.conn.onerror = null;
      if (this.conn.readyState === WebSocket.OPEN || this.conn.readyState === WebSocket.CONNECTING) {
        this.conn.close();
      }
      this.conn = null;
    }

    this.token = token;
    this.cryptoSession = null;
    this.encrypted = false;
    this.reconnectAttempts = 0;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${window.location.host}/ws/browser?token=${token}`;

    this.conn = new WebSocket(url);
    // Set binary type to arraybuffer for encrypted frames
    this.conn.binaryType = 'arraybuffer';

    this.conn.onopen = () => {
      this.reconnectAttempts = 0;
      // Don't start ping or fire connected yet — wait for key exchange
    };

    this.conn.onmessage = async (event) => {
      try {
        // Handle binary (encrypted) messages
        if (event.data instanceof ArrayBuffer) {
          await this.handleBinaryMessage(event.data);
          return;
        }

        const raw = event.data as string;

        // Check for encryption hello from server
        try {
          const parsed = JSON.parse(raw);
          if (parsed.type === 'encryption_hello') {
            await this.performKeyExchange(parsed);
            return;
          }
        } catch { /* not JSON, continue */ }

        // Handle pong for latency measurement
        try {
          const parsed = JSON.parse(raw);
          if (parsed.type === 'pong' && parsed.ping_ts) {
            this.latency = Date.now() - parsed.ping_ts;
            this.latencyCallback?.(this.latency);
            return;
          }
        } catch { /* not JSON or not pong, continue */ }

        const env: Envelope = JSON.parse(raw);
        this.handleMessage(env);
      } catch (e) {
        console.error('Failed to parse WebSocket message', e);
      }
    };

    this.conn.onclose = () => {
      this.onConnectionChange?.(false);
      this.latency = -1;
      this.latencyCallback?.(-1);
      this.stopPing();
      this.stopDummy();
      this.attemptReconnect();
    };

    this.conn.onerror = () => {
      this.conn?.close();
    };
  }

  disconnect() {
    this.reconnectAttempts = this.maxReconnectAttempts;
    this.stopPing();
    this.stopDummy();
    this.conn?.close();
    this.conn = null;
  }

  send(env: Envelope) {
    if (!this.conn || this.conn.readyState !== WebSocket.OPEN) return;

    if (this.cryptoSession && this.encrypted) {
      // Queue to preserve ordering — encrypt+send sequentially
      this.sendQueue.push(env);
      this.drainSendQueue();
    } else {
      this.conn.send(JSON.stringify(env));
    }
  }

  private drainSendQueue() {
    if (this.sending || this.sendQueue.length === 0) return;
    this.sending = true;

    const env = this.sendQueue.shift()!;
    this.sendChain = this.sendChain.then(async () => {
      if (this.conn?.readyState === WebSocket.OPEN && this.cryptoSession) {
        try {
          const frame = await this.cryptoSession.encryptFrame(env);
          this.conn!.send(frame);
        } catch (e) {
          console.error('Encrypt error:', e);
        }
      }
    }).finally(() => {
      this.sending = false;
      this.drainSendQueue();
    });
  }

  request(env: Envelope, timeout = 30000): Promise<Envelope> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(env.id);
        reject(new Error('Request timeout'));
      }, timeout);

      this.pending.set(env.id, { resolve, reject, timer });
      this.send(env);
    });
  }

  on(action: string, handler: MessageHandler) {
    if (!this.handlers.has(action)) {
      this.handlers.set(action, []);
    }
    this.handlers.get(action)!.push(handler);
  }

  off(action: string, handler?: MessageHandler) {
    if (!handler) {
      this.handlers.delete(action);
    } else {
      const list = this.handlers.get(action);
      if (list) {
        const idx = list.indexOf(handler);
        if (idx >= 0) list.splice(idx, 1);
        if (list.length === 0) this.handlers.delete(action);
      }
    }
  }

  setOnConnectionChange(fn: (connected: boolean) => void) {
    this.onConnectionChange = fn;
  }

  setOnLatencyChange(fn: (ms: number) => void) {
    this.latencyCallback = fn;
  }

  getLatency(): number {
    return this.latency;
  }

  isConnected(): boolean {
    return this.conn?.readyState === WebSocket.OPEN;
  }

  isEncrypted(): boolean {
    return this.encrypted;
  }

  private handleMessage(env: Envelope) {
    // Route to pending requests
    if (env.type === 'response') {
      const pending = this.pending.get(env.id);
      if (pending) {
        pending.resolve(env);
        clearTimeout(pending.timer);
        this.pending.delete(env.id);
        return;
      }
    }

    // Stream messages always go to action handlers
    if (env.type === 'stream') {
      const handlers = this.handlers.get(env.action) || [];
      handlers.forEach((h) => h(env));
      return;
    }

    // Route to event handlers
    const handlers = this.handlers.get(env.action) || [];
    handlers.forEach((h) => h(env));

    // Wildcard handlers
    const wildcardHandlers = this.handlers.get('*') || [];
    wildcardHandlers.forEach((h) => h(env));
  }

  private async handleBinaryMessage(data: ArrayBuffer) {
    if (!this.cryptoSession) {
      console.warn('Received binary frame without crypto session');
      return;
    }

    try {
      const { env, isDummy } = await this.cryptoSession.decryptFrame(data);
      if (isDummy || !env) return;
      this.handleMessage(env);
    } catch (e) {
      console.error('Decrypt error:', e);
    }
  }

  private async performKeyExchange(serverHello: { server_pub_key: string; server_nonce: string }) {
    // Pre-check: verify secure context before attempting key exchange
    const secureContextError = checkSecureContext();
    if (secureContextError) {
      // Show alert to user with clear instructions
      alert(`加密连接失败\n\n${secureContextError}\n\n请使用以下方式之一访问服务器:\n• HTTPS 连接\n• http://localhost:${window.location.port}\n• http://127.0.0.1:${window.location.port}`);
      // Close connection - server requires encryption
      if (this.conn) {
        this.conn.close();
      }
      return;
    }

    try {
      if (!this.conn) return;

      this.cryptoSession = await CryptoSession.handshake(this.conn, this.token, serverHello);
      this.encrypted = true;

      console.log('Encryption established with server');

      // Now we're fully connected
      this.startPing();
      this.startDummy();
      this.onConnectionChange?.(true);
    } catch (e: any) {
      // Check if this is a secure context error
      if (e.message && e.message.includes('secure context')) {
        alert(`加密连接失败\n\n${e.message}\n\n请使用 HTTPS 或 localhost 访问服务器。`);
        if (this.conn) {
          this.conn.close();
        }
        return;
      }

      console.error('Key exchange failed:', e);
      // Server requires encryption - no fallback available
      if (this.conn) {
        this.conn.close();
      }
      this.onConnectionChange?.(false);
    }
  }

  private startPing() {
    // Jittered ping interval: 12-18 seconds
    const baseInterval = 15000;
    const jitteredInterval = baseInterval + (Math.random() * 6000 - 3000);

    this.pingTimer = setInterval(() => {
      if (this.conn?.readyState === WebSocket.OPEN) {
        if (this.cryptoSession && this.encrypted) {
          // Route encrypted ping through send() to preserve seqNum ordering
          const pingEnv: Envelope = {
            id: crypto.randomUUID(),
            channel: 'ping',
            type: 'request' as const,
            action: 'ping',
            payload: { ping_ts: Date.now() },
            ts: Date.now(),
          };
          this.send(pingEnv);
        } else {
          this.conn.send(JSON.stringify({ type: 'ping', ping_ts: Date.now() }));
        }
      } else if (this.pingTimer) {
        clearInterval(this.pingTimer);
        this.pingTimer = null;
      }
    }, jitteredInterval);
  }

  private stopPing() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private startDummy() {
    if (!this.cryptoSession || !this.encrypted) return;

    // Random interval 5-15 seconds
    const scheduleNext = () => {
      const delay = 5000 + Math.random() * 10000;
      this.dummyTimer = setTimeout(() => {
        if (!this.cryptoSession || !this.encrypted || !this.conn || this.conn.readyState !== WebSocket.OPEN) {
          return;
        }
        // Route dummy through sendChain to preserve seqNum ordering
        this.sendChain = this.sendChain.then(async () => {
          if (this.cryptoSession && this.encrypted && this.conn?.readyState === WebSocket.OPEN) {
            try {
              const frame = await this.cryptoSession.encryptDummy();
              this.conn!.send(frame);
            } catch {}
          }
        });
        scheduleNext();
      }, delay) as any;
    };
    scheduleNext();
  }

  private stopDummy() {
    if (this.dummyTimer) {
      clearTimeout(this.dummyTimer);
      this.dummyTimer = null;
    }
  }

  private attemptReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) return;

    this.reconnectAttempts++;
    const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 30000);

    setTimeout(() => {
      if (this.token) {
        this.connect(this.token);
      }
    }, delay);
  }
}

export const wsManager = new WebSocketManager();
export default wsManager;
