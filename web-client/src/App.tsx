import { useEffect, useRef } from 'react';
import { assetManager } from './engine/AssetManager';
import { GameCanvas } from './engine/GameCanvas';
import { initGameSessionBindings } from './net/gameSession';
import { useSessionStore } from './store/sessionStore';
import { useWorldStore } from './store/worldStore';
import { LoginScreen } from './ui/screens/LoginScreen';
import { CharSelectScreen } from './ui/screens/CharSelectScreen';
import { WorldHUD } from './ui/hud/WorldHUD';

function WorldView() {
  const hostRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = hostRef.current;
    if (!el) return;
    const canvas = new GameCanvas();
    void canvas.mount(el);
    return () => {
      canvas.destroy();
    };
  }, []);

  return (
    <div className="relative h-full w-full overflow-hidden bg-slate-950">
      <div ref={hostRef} className="h-full w-full" />
      <WorldHUD />
    </div>
  );
}

export function App() {
  const screen = useSessionStore((s) => s.screen);
  const setMapsCatalog = useWorldStore((s) => s.setMapsCatalog);

  useEffect(() => {
    initGameSessionBindings();
    void assetManager.ensureCatalogs().then(({ maps }) => {
      setMapsCatalog(maps);
    });
  }, [setMapsCatalog]);

  if (screen === 'login') {
    return <LoginScreen />;
  }
  if (screen === 'charSelect') {
    return <CharSelectScreen />;
  }
  return <WorldView />;
}
