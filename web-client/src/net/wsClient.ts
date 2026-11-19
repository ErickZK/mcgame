import { OutgoingEnvelopeSchema, type ServerEnvelope } from './protocol';

type EventHandler = (args: unknown[]) => void;

interface PendingCall {
  resolve: (data: unknown) => void;
  reject: (err: Error) => void;
  timer: number;
}

export class GameWSClient {
  private ws: WebSocket | null = null;
  private nextTx = 1;
  private pending = new Map<number, PendingCall>();
  private listeners = new Map<string, Set<EventHandler>>();
  private pingTimer: number | null = null;
  private onLatency?: (ms: number) => void;
  private onDisconnect?: (reason: string) => void;

  setCallbacks(cbs: { onLatency?: (ms: number) => void; onDisconnect?: (reason: string) => void }) {
    this.onLatency = cbs.onLatency;
    this.onDisconnect = cbs.onDisconnect;
  }

  async connect(ticket: string, app = 'tcn/line1'): Promise<void> {
    this.disconnect();
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/ws`;

    await new Promise<void>((resolve, reject) => {
      const socket = new WebSocket(wsUrl);
      this.ws = socket;

      let settled = false;
      const fail = (err: Error) => {
        if (!settled) {
          settled = true;
          reject(err);
        }
      };

      socket.onopen = async () => {
        try {
          await this.sendFrameAndWait({ op: 'auth', ticket, app }, 10000);
          settled = true;
          this.startHeartbeat();
          resolve();
        } catch (err) {
          socket.close();
          fail(err instanceof Error ? err : new Error('Falha na autenticação WebSocket'));
        }
      };

      socket.onmessage = (evt) => {
        if (typeof evt.data !== 'string') return;
        this.handleRawMessage(evt.data);
      };

      socket.onerror = () => {
        fail(new Error('Não foi possível conectar ao servidor WebSocket'));
      };

      socket.onclose = (ev) => {
        this.stopHeartbeat();
        this.rejectAllPending(new Error('Conexão encerrada'));
        if (!settled) {
          fail(new Error(`Conexão fechada (${ev.code})`));
        } else {
          this.onDisconnect?.(ev.reason || `Desconectado (${ev.code})`);
        }
      };
    });
  }

  disconnect() {
    this.stopHeartbeat();
    if (this.ws) {
      this.ws.onclose = null;
      this.ws.close();
      this.ws = null;
    }
    this.rejectAllPending(new Error('Desconectado'));
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }

  call<T = unknown>(method: string, args: unknown[] = [], timeoutMs = 10000): Promise<T> {
    return this.sendFrameAndWait({ op: 'rpc', m: method, a: args }, timeoutMs) as Promise<T>;
  }

  notify(method: string, args: unknown[] = []) {
    if (!this.isConnected() || !this.ws) return;
    this.ws.send(JSON.stringify({ op: 'rpc', tx: 0, m: method, a: args }));
  }

  on(eventName: string, handler: EventHandler): () => void {
    let set = this.listeners.get(eventName);
    if (!set) {
      set = new Set();
      this.listeners.set(eventName, set);
    }
    set.add(handler);
    return () => {
      set?.delete(handler);
    };
  }

  private sendFrameAndWait(body: Record<string, unknown>, timeoutMs: number): Promise<unknown> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('WebSocket não conectado'));
    }
    const tx = this.nextTx++;
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(tx);
        reject(new Error(`Timeout aguardando resposta de ${String(body.m ?? body.op)}`));
      }, timeoutMs);

      this.pending.set(tx, { resolve, reject, timer });
      this.ws!.send(JSON.stringify({ ...body, tx }));
    });
  }

  private handleRawMessage(raw: string) {
    let parsed: ServerEnvelope;
    try {
      const json = JSON.parse(raw);
      const checked = OutgoingEnvelopeSchema.safeParse(json);
      if (!checked.success) return;
      parsed = checked.data;
    } catch {
      return;
    }

    if (parsed.op === 'res' && parsed.tx !== undefined) {
      const p = this.pending.get(parsed.tx);
      if (p) {
        window.clearTimeout(p.timer);
        this.pending.delete(parsed.tx);
        p.resolve(parsed.d);
      }
      return;
    }

    if (parsed.op === 'err' && parsed.tx !== undefined) {
      const p = this.pending.get(parsed.tx);
      if (p) {
        window.clearTimeout(p.timer);
        this.pending.delete(parsed.tx);
        p.reject(new Error(parsed.e || 'Erro RPC'));
      }
      return;
    }

    if (parsed.op === 'evt' && parsed.m) {
      const set = this.listeners.get(parsed.m);
      if (set) {
        const args = parsed.a ?? [];
        for (const fn of set) {
          fn(args);
        }
      }
    }
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.pingTimer = window.setInterval(async () => {
      if (!this.isConnected()) return;
      const t0 = performance.now();
      try {
        await this.sendFrameAndWait({ op: 'ping' }, 5000);
        this.onLatency?.(Math.round(performance.now() - t0));
      } catch {
        // heartbeat timeout handled by server read deadline
      }
    }, 20000);
  }

  private stopHeartbeat() {
    if (this.pingTimer !== null) {
      window.clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private rejectAllPending(err: Error) {
    for (const [, p] of this.pending) {
      window.clearTimeout(p.timer);
      p.reject(err);
    }
    this.pending.clear();
  }
}

export const wsClient = new GameWSClient();
