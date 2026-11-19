import { useState, useRef, useEffect, type FormEvent } from 'react';
import {
  Send,
  LogOut,
  Users,
  Wifi,
  WifiOff,
  X,
  ArrowUp,
  ArrowDown,
  ArrowLeft,
  ArrowRight,
} from 'lucide-react';
import { useSessionStore } from '../../store/sessionStore';
import { useWorldStore } from '../../store/worldStore';
import { sendChatMessage, sendPlayerMove } from '../../net/gameSession';
import { wsClient } from '../../net/wsClient';
import { MiniMap } from './MiniMap';
import { WorldMapModal } from './WorldMapModal';

export function WorldHUD() {
  const offlineDemo = useSessionStore((s) => s.offlineDemo);
  const connected = useSessionStore((s) => s.connected);
  const latencyMs = useSessionStore((s) => s.latencyMs);
  const toast = useSessionStore((s) => s.toast);
  const showToast = useSessionStore((s) => s.showToast);
  const resetSession = useSessionStore((s) => s.resetSession);

  const self = useWorldStore((s) => s.self);
  const players = useWorldStore((s) => s.players);
  const npcs = useWorldStore((s) => s.npcs);
  const chatMessages = useWorldStore((s) => s.chatMessages);
  const selectedNpc = useWorldStore((s) => s.selectedNpc);
  const setSelectedNpc = useWorldStore((s) => s.setSelectedNpc);
  const resetWorld = useWorldStore((s) => s.resetWorld);

  const [chatText, setChatText] = useState('');
  const [channelId, setChannelId] = useState(0);
  const chatEndRef = useRef<HTMLDivElement>(null);

  const hpPct = self && self.maxHp ? Math.min(100, Math.round(((self.hp ?? 0) / self.maxHp) * 100)) : 100;
  const mpPct = self && self.maxMp ? Math.min(100, Math.round(((self.mp ?? 0) / self.maxMp) * 100)) : 100;

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chatMessages.length]);

  useEffect(() => {
    if (!toast) return;
    const t = window.setTimeout(() => showToast(null), 4000);
    return () => window.clearTimeout(t);
  }, [toast, showToast]);

  function handleSendChat(e: FormEvent) {
    e.preventDefault();
    if (!chatText.trim()) return;
    sendChatMessage(channelId, chatText);
    setChatText('');
  }

  function handleExit() {
    wsClient.disconnect();
    resetWorld();
    resetSession();
  }

  function nudge(dx: number, dy: number) {
    if (!self) return;
    sendPlayerMove(self.targetX + dx, self.targetY + dy);
  }

  return (
    <div className="pointer-events-none fixed inset-0 z-20 flex flex-col justify-between p-3">
      <WorldMapModal />

      {toast && (
        <div className="pointer-events-auto fixed left-1/2 top-4 z-30 -translate-x-1/2 rounded-full border border-cyan-400/40 bg-slate-900/95 px-4 py-1.5 text-xs font-medium text-cyan-100 shadow-lg">
          {toast}
        </div>
      )}

      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="pointer-events-auto w-64 rounded-xl border border-slate-700/80 bg-slate-950/85 p-3 shadow-xl backdrop-blur-md">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-sm font-bold text-white">
              {self?.name ?? 'Carregando...'}
            </span>
            <div className="flex items-center gap-1.5">
              <span className="rounded bg-indigo-900/80 px-1.5 py-0.5 text-[11px] font-semibold text-indigo-200">
                Nv. {self?.level ?? 1}
              </span>
              <button
                type="button"
                onClick={handleExit}
                title="Sair para login"
                className="rounded border border-slate-700 p-1 text-slate-400 hover:border-rose-500/60 hover:text-rose-200"
              >
                <LogOut className="h-3 w-3" />
              </button>
            </div>
          </div>

          <div className="mt-2 space-y-1.5">
            <div>
              <div className="mb-0.5 flex justify-between text-[10px] text-rose-200">
                <span>HP</span>
                <span>
                  {self?.hp ?? 0}/{self?.maxHp ?? 0}
                </span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-slate-800">
                <div className="h-full bg-gradient-to-r from-rose-600 to-rose-400" style={{ width: `${hpPct}%` }} />
              </div>
            </div>

            <div>
              <div className="mb-0.5 flex justify-between text-[10px] text-sky-200">
                <span>MP</span>
                <span>
                  {self?.mp ?? 0}/{self?.maxMp ?? 0}
                </span>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-slate-800">
                <div className="h-full bg-gradient-to-r from-sky-600 to-cyan-400" style={{ width: `${mpPct}%` }} />
              </div>
            </div>
          </div>

          <div className="mt-2.5 flex items-center justify-between text-[11px] text-slate-400">
            <span className="flex items-center gap-1">
              {offlineDemo ? (
                <>
                  <WifiOff className="h-3 w-3 text-amber-400" /> Preview Local
                </>
              ) : connected ? (
                <>
                  <Wifi className="h-3 w-3 text-emerald-400" /> {latencyMs > 0 ? `${latencyMs}ms` : 'Online'}
                </>
              ) : (
                <>
                  <WifiOff className="h-3 w-3 text-rose-400" /> Reconectando
                </>
              )}
            </span>
            <span className="flex items-center gap-1">
              <Users className="h-3 w-3 text-cyan-400" /> {Object.keys(players).length + 1} • NPCs {Object.keys(npcs).length}
            </span>
          </div>
        </div>

        <MiniMap />
      </div>

      {selectedNpc && (
        <div className="pointer-events-auto mx-auto w-full max-w-md rounded-xl border border-amber-400/40 bg-slate-950/92 p-4 shadow-2xl backdrop-blur-md">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-bold text-amber-300">{selectedNpc.name}</h3>
            <button
              type="button"
              onClick={() => setSelectedNpc(null)}
              className="rounded p-1 text-slate-400 hover:bg-slate-800 hover:text-white"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <p className="mt-2 text-xs text-slate-300">
            NPC #{selectedNpc.id} (resCode {selectedNpc.resCode}) em ({selectedNpc.x}, {selectedNpc.y}). Diálogos, lojas e missões entram no Bloco 2.
          </p>
        </div>
      )}

      <div className="flex items-end justify-between gap-3">
        <div className="pointer-events-auto flex w-full max-w-sm flex-col rounded-xl border border-slate-700/80 bg-slate-950/85 shadow-xl backdrop-blur-md">
          <div className="flex items-center gap-1 border-b border-slate-800 px-2 py-1 text-[11px]">
            {[
              { id: 0, label: 'Local' },
              { id: 1, label: 'Mundo' },
            ].map((tab) => (
              <button
                key={tab.id}
                type="button"
                onClick={() => setChannelId(tab.id)}
                className={`rounded px-2 py-0.5 font-medium ${
                  channelId === tab.id
                    ? 'bg-indigo-600 text-white'
                    : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <div className="h-32 overflow-y-auto px-3 py-2 text-xs">
            {chatMessages.map((m) => (
              <div key={m.id} className="mb-1 break-words leading-snug">
                {m.system ? (
                  <span className="text-amber-300">[{m.sender}] {m.text}</span>
                ) : (
                  <>
                    <span className="font-semibold text-cyan-300">{m.sender}: </span>
                    <span className="text-slate-200">{m.text}</span>
                  </>
                )}
              </div>
            ))}
            <div ref={chatEndRef} />
          </div>

          <form onSubmit={handleSendChat} className="flex border-t border-slate-800 p-1.5">
            <input
              type="text"
              maxLength={200}
              value={chatText}
              onChange={(e) => setChatText(e.target.value)}
              placeholder="Digite sua mensagem..."
              className="flex-1 bg-transparent px-2 text-xs text-white outline-none placeholder:text-slate-500"
            />
            <button
              type="submit"
              className="rounded-lg bg-indigo-600 px-2.5 py-1 text-white hover:bg-indigo-500"
            >
              <Send className="h-3.5 w-3.5" />
            </button>
          </form>
        </div>

        <div className="pointer-events-auto grid grid-cols-3 gap-1 md:hidden">
          <div />
          <button
            type="button"
            onClick={() => nudge(0, -140)}
            className="flex h-10 w-10 items-center justify-center rounded-lg border border-slate-700 bg-slate-950/85 text-slate-200 active:bg-indigo-600"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
          <div />
          <button
            type="button"
            onClick={() => nudge(-140, 0)}
            className="flex h-10 w-10 items-center justify-center rounded-lg border border-slate-700 bg-slate-950/85 text-slate-200 active:bg-indigo-600"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={() => nudge(0, 140)}
            className="flex h-10 w-10 items-center justify-center rounded-lg border border-slate-700 bg-slate-950/85 text-slate-200 active:bg-indigo-600"
          >
            <ArrowDown className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={() => nudge(140, 0)}
            className="flex h-10 w-10 items-center justify-center rounded-lg border border-slate-700 bg-slate-950/85 text-slate-200 active:bg-indigo-600"
          >
            <ArrowRight className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  );
}
