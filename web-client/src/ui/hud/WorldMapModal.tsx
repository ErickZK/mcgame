import { useState } from 'react';
import { Compass, Navigation, Search, X } from 'lucide-react';
import { useWorldStore } from '../../store/worldStore';
import { teleportToMap } from '../../net/gameSession';

export function WorldMapModal() {
  const open = useWorldStore((s) => s.worldMapOpen);
  const setOpen = useWorldStore((s) => s.setWorldMapOpen);
  const mapsCatalog = useWorldStore((s) => s.mapsCatalog);
  const currentMapId = useWorldStore((s) => s.currentMapId);
  const requestNavigateTo = useWorldStore((s) => s.requestNavigateTo);
  const setPendingPortal = useWorldStore((s) => s.setPendingPortal);

  const [query, setQuery] = useState('');

  if (!open) return null;

  const currentMap = mapsCatalog.find((m) => m.id === currentMapId);
  const filtered = mapsCatalog.filter((m) => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return m.name.toLowerCase().includes(q) || String(m.id).includes(q);
  });

  return (
    <div className="pointer-events-auto fixed inset-0 z-40 flex items-center justify-center bg-slate-950/75 p-4 backdrop-blur-xs">
      <div className="flex max-h-[85vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-indigo-400/30 bg-slate-900/95 shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-800 px-5 py-3">
          <div className="flex items-center gap-2">
            <Compass className="h-5 w-5 text-cyan-400" />
            <h2 className="text-base font-bold text-white">Mapa-Múndi & Portais</h2>
            <span className="rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-300">
              Atual: {currentMap?.name ?? `#${currentMapId}`}
            </span>
          </div>
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="grid flex-1 grid-cols-1 gap-4 overflow-hidden p-4 md:grid-cols-5">
          <div className="flex flex-col gap-3 md:col-span-3">
            <div className="relative overflow-hidden rounded-xl border border-slate-800 bg-slate-950">
              <img
                src="/assets/ui/worldmap1.jpg"
                alt="Mapa-Múndi"
                className="h-56 w-full object-cover md:h-64"
              />
              <div className="absolute bottom-2 left-2 rounded-md bg-slate-950/85 px-2.5 py-1 text-[11px] text-slate-200">
                {mapsCatalog.length} mapas extraídos com colisão A* e portais reais
              </div>
            </div>

            <div className="rounded-xl border border-slate-800 bg-slate-950/70 p-3">
              <div className="mb-2 text-xs font-semibold text-fuchsia-300">
                Portais deste mapa ({currentMap?.portals?.length ?? 0})
              </div>
              {currentMap?.portals && currentMap.portals.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {currentMap.portals.map((p) => {
                    const dest = mapsCatalog.find((m) => m.id === p.destMapId);
                    return (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => {
                          setPendingPortal(p);
                          requestNavigateTo({ x: p.x + 91, y: p.y + 68 });
                          setOpen(false);
                        }}
                        className="flex items-center gap-1.5 rounded-lg border border-fuchsia-500/35 bg-fuchsia-950/40 px-2.5 py-1.5 text-xs text-fuchsia-200 hover:bg-fuchsia-900/50"
                      >
                        <Navigation className="h-3.5 w-3.5" />
                        <span>{dest?.name ?? p.name}</span>
                      </button>
                    );
                  })}
                </div>
              ) : (
                <p className="text-xs text-slate-500">
                  Nenhum portal mapeado nesta sala. Use a lista ao lado para viajar.
                </p>
              )}
            </div>
          </div>

          <div className="flex flex-col overflow-hidden rounded-xl border border-slate-800 bg-slate-950/80 md:col-span-2">
            <div className="flex items-center gap-2 border-b border-slate-800 px-3 py-2">
              <Search className="h-3.5 w-3.5 text-slate-400" />
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Filtrar mapa por nome ou ID..."
                className="w-full bg-transparent text-xs text-white outline-none placeholder:text-slate-500"
              />
            </div>

            <div className="flex-1 overflow-y-auto p-2">
              {filtered.map((m) => {
                const isCurrent = m.id === currentMapId;
                return (
                  <button
                    key={m.id}
                    type="button"
                    onClick={() => void teleportToMap(m.id, m.safeX, m.safeY)}
                    className={`mb-1 flex w-full items-center justify-between rounded-lg px-2.5 py-2 text-left text-xs transition ${
                      isCurrent
                        ? 'bg-indigo-600/30 text-cyan-200'
                        : 'text-slate-300 hover:bg-slate-800/80'
                    }`}
                  >
                    <span className="truncate font-medium">
                      #{m.id} {m.name}
                    </span>
                    <span className="ml-2 shrink-0 text-[10px] text-slate-500">
                      {m.portals?.length ? `${m.portals.length}P` : ''}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
