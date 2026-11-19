import { type MouseEvent } from 'react';
import { Globe, MapPin } from 'lucide-react';
import { useWorldStore } from '../../store/worldStore';

export function MiniMap() {
  const mapsCatalog = useWorldStore((s) => s.mapsCatalog);
  const currentMapId = useWorldStore((s) => s.currentMapId);
  const self = useWorldStore((s) => s.self);
  const players = useWorldStore((s) => s.players);
  const npcs = useWorldStore((s) => s.npcs);
  const requestNavigateTo = useWorldStore((s) => s.requestNavigateTo);
  const setWorldMapOpen = useWorldStore((s) => s.setWorldMapOpen);

  const currentMap = mapsCatalog.find((m) => m.id === currentMapId);
  const mapW = currentMap?.width ?? 2000;
  const mapH = currentMap?.height ?? 1500;

  function handleMiniClick(e: MouseEvent<HTMLDivElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const rx = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const ry = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height));
    requestNavigateTo({ x: Math.round(rx * mapW), y: Math.round(ry * mapH) });
  }

  const pct = (val: number, max: number) => `${Math.max(0, Math.min(100, (val / max) * 100))}%`;

  return (
    <div className="pointer-events-auto w-48 overflow-hidden rounded-xl border border-slate-700/80 bg-slate-950/90 shadow-xl backdrop-blur-md">
      <div className="flex items-center justify-between border-b border-slate-800 px-2.5 py-1.5">
        <div className="flex min-w-0 items-center gap-1">
          <MapPin className="h-3.5 w-3.5 shrink-0 text-amber-400" />
          <span className="truncate text-[11px] font-semibold text-white">
            {currentMap?.name ?? `Mapa #${currentMapId}`}
          </span>
        </div>
        <button
          type="button"
          onClick={() => setWorldMapOpen(true)}
          title="Abrir Mapa-Múndi (M)"
          className="flex items-center gap-1 rounded bg-indigo-600/80 px-1.5 py-0.5 text-[10px] font-semibold text-white hover:bg-indigo-500"
        >
          <Globe className="h-3 w-3" /> M
        </button>
      </div>

      <div
        onClick={handleMiniClick}
        title="Clique para caminhar até este ponto (A*)"
        className="relative h-32 w-full cursor-crosshair overflow-hidden bg-slate-900"
      >
        {currentMap?.image && (
          <img
            src={currentMap.image}
            alt={currentMap.name}
            draggable={false}
            className="h-full w-full object-cover opacity-80"
          />
        )}

        {currentMap?.portals?.map((p) => (
          <span
            key={`p-${p.id}`}
            title={`Portal: ${p.name}`}
            className="pointer-events-none absolute h-2.5 w-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border border-white bg-fuchsia-400 shadow"
            style={{ left: pct(p.x + 91, mapW), top: pct(p.y + 68, mapH) }}
          />
        ))}

        {Object.values(npcs).map((n) => (
          <span
            key={`n-${n.id}`}
            className="pointer-events-none absolute h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-amber-400"
            style={{ left: pct(n.x, mapW), top: pct(n.y, mapH) }}
          />
        ))}

        {Object.values(players).map((p) => (
          <span
            key={`pl-${p.id}`}
            className="pointer-events-none absolute h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-sky-400"
            style={{ left: pct(p.targetX, mapW), top: pct(p.targetY, mapH) }}
          />
        ))}

        {self && (
          <span
            className="pointer-events-none absolute h-2.5 w-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border border-slate-950 bg-emerald-400 shadow"
            style={{ left: pct(self.targetX, mapW), top: pct(self.targetY, mapH) }}
          />
        )}
      </div>

      <div className="flex items-center justify-between px-2.5 py-1 text-[10px] text-slate-400">
        <span>
          X:{Math.round(self?.targetX ?? 0)} Y:{Math.round(self?.targetY ?? 0)}
        </span>
        <span>{currentMap?.collisionBin ? 'A* Ativo' : 'Livre'}</span>
      </div>
    </div>
  );
}
