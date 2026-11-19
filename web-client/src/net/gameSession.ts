import { wsClient } from './wsClient';
import { type CharacterSummary, type Point2D, type PortalItem, type WorldEntity } from './protocol';
import { useSessionStore } from '../store/sessionStore';
import { useWorldStore } from '../store/worldStore';

let initialized = false;
let lastMoveSentAt = 0;
let lastPortalTriggerAt = 0;

function toNum(v: unknown, fallback = 0): number {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v === 'string') {
    const parsed = Number(v);
    if (Number.isFinite(parsed)) return parsed;
  }
  return fallback;
}

function toStr(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback;
}

function parseRoute(raw: unknown): Point2D[] | undefined {
  if (!Array.isArray(raw) || raw.length === 0) return undefined;
  const pts: Point2D[] = [];
  for (const item of raw) {
    if (Array.isArray(item) && item.length >= 2) {
      const x = toNum(item[0], -1);
      const y = toNum(item[1], -1);
      if (x >= 0 && y >= 0) pts.push({ x, y });
    }
  }
  return pts.length > 0 ? pts : undefined;
}

function parsePlayerObject(raw: Record<string, unknown>, kind: 'self' | 'player'): WorldEntity {
  const id = toNum(raw.id ?? raw.cid, 0);
  const x = toNum(raw.x ?? raw.posX, 500);
  const y = toNum(raw.y ?? raw.posY, 500);
  return {
    id,
    name: toStr(raw.name, `Herói #${id}`),
    x,
    y,
    targetX: x,
    targetY: y,
    path: [],
    level: toNum(raw.lv ?? raw.level, 1),
    classId: toNum(raw.cls ?? raw.classId, 1),
    gender: toNum(raw.sex ?? raw.gender, 0),
    resCode: toNum(raw.resCode ?? raw.rc, 0),
    kind,
    hp: toNum(raw.hp, 100),
    maxHp: Math.max(1, toNum(raw.maxHp ?? raw.hpMax, 100)),
    mp: toNum(raw.mp, 100),
    maxMp: Math.max(1, toNum(raw.maxMp ?? raw.mpMax, 100)),
  };
}

export function initGameSessionBindings() {
  if (initialized) return;
  initialized = true;

  wsClient.setCallbacks({
    onLatency: (ms) => useSessionStore.getState().setLatency(ms),
    onDisconnect: (reason) => {
      const session = useSessionStore.getState();
      if (!session.offlineDemo) {
        session.setConnected(false);
        session.showToast(`Conexão perdida: ${reason}`);
      }
    },
  });

  wsClient.on('onIcl', (args) => {
    const list = Array.isArray(args[0]) ? args[0] : [];
    const chars: CharacterSummary[] = list
      .filter((item): item is Record<string, unknown> => Boolean(item && typeof item === 'object'))
      .map((item) => ({
        id: toNum(item.id, 0),
        name: toStr(item.name, 'Sem nome'),
        level: toNum(item.level, 1),
        exp: toNum(item.exp, 0),
        classId: toNum(item.classId, 1),
        gender: toNum(item.gender, 0),
        iconCode: toNum(item.iconCode, 0),
      }));
    useSessionStore.getState().setCharacters(chars);
  });

  wsClient.on('onSceneLogin', (args) => {
    const mapId = toNum(args[0], 1);
    useWorldStore.getState().setCurrentMapId(mapId);
    useSessionStore.getState().setScreen('world');
  });

  wsClient.on('onSceneEnter', (args) => {
    const mapId = toNum(args[0], 1);
    const x = toNum(args[1], 500);
    const y = toNum(args[2], 500);
    const ws = useWorldStore.getState();
    ws.setCurrentMapId(mapId);
    ws.updateSelfPosition(x, y, true, []);
    wsClient.notify('createNpcs', []);
    wsClient.notify('createChars', []);
  });

  wsClient.on('onCreateCharactor', (args) => {
    const raw = args[0];
    if (raw && typeof raw === 'object') {
      const entity = parsePlayerObject(raw as Record<string, unknown>, 'self');
      useWorldStore.getState().setSelf(entity);
    }
  });

  wsClient.on('onCreateChars', (args) => {
    const raw = args[0];
    if (!raw || typeof raw !== 'object') return;
    const selfId = useWorldStore.getState().self?.id;
    const entries = Array.isArray(raw) ? raw : Object.values(raw as Record<string, unknown>);
    for (const item of entries) {
      if (!item || typeof item !== 'object') continue;
      const entity = parsePlayerObject(item as Record<string, unknown>, 'player');
      if (entity.id > 0 && entity.id !== selfId) {
        useWorldStore.getState().upsertPlayer(entity);
      }
    }
  });

  wsClient.on('onScenePlayerEntered', (args) => {
    const raw = args[0];
    if (!raw || typeof raw !== 'object') return;
    const entity = parsePlayerObject(raw as Record<string, unknown>, 'player');
    if (entity.id > 0 && entity.id !== useWorldStore.getState().self?.id) {
      useWorldStore.getState().upsertPlayer(entity);
    }
  });

  wsClient.on('onCharLeaveScene', (args) => {
    const charId = toNum(args[0], 0);
    if (charId > 0) {
      useWorldStore.getState().removePlayer(charId);
    }
  });

  wsClient.on('onCreateNpcs', (args) => {
    const raw = args[0];
    if (!raw || typeof raw !== 'object') return;
    const npcs: Record<number, WorldEntity> = {};
    for (const [key, val] of Object.entries(raw as Record<string, unknown>)) {
      if (!val || typeof val !== 'object') continue;
      const rec = val as Record<string, unknown>;
      const id = toNum(rec.nid ?? key, 0);
      const x = toNum(rec.x, 400);
      const y = toNum(rec.y, 400);
      npcs[id] = {
        id,
        name: toStr(rec.name, `NPC #${id}`),
        x,
        y,
        targetX: x,
        targetY: y,
        level: 0,
        classId: 0,
        gender: 0,
        resCode: toNum(rec.resCode, 0),
        kind: 'npc',
      };
    }
    useWorldStore.getState().setNpcs(npcs);
  });

  wsClient.on('onCR', (args) => {
    const raw = args[0];
    if (!raw || typeof raw !== 'object') return;
    const rec = raw as Record<string, unknown>;
    const id = toNum(rec.id, 0);
    const x = toNum(rec.x, -1);
    const y = toNum(rec.y, -1);
    if (id > 0 && x >= 0 && y >= 0) {
      const route = parseRoute(rec.route);
      useWorldStore.getState().moveEntity(id, x, y, route);
    }
  });

  wsClient.on('onSay', (args) => {
    const raw = args[0];
    if (!raw || typeof raw !== 'object') return;
    const rec = raw as Record<string, unknown>;
    useWorldStore.getState().pushChat({
      channelId: toNum(rec.channelId, 0),
      sender: toStr(rec.name, 'Jogador'),
      text: toStr(rec.msg, ''),
    });
  });

  const handleSystemNote = (args: unknown[]) => {
    const text = toStr(args[0], '');
    if (!text) return;
    useWorldStore.getState().pushChat({
      channelId: 99,
      sender: 'Sistema',
      text,
      system: true,
    });
    useSessionStore.getState().showToast(text);
  };

  wsClient.on('onMidNote', handleSystemNote);
  wsClient.on('onSystemMidMsgOrNote', handleSystemNote);
}

export async function enterWorldWithCharacter(char: CharacterSummary) {
  await wsClient.call('chooseCharactor', [char.id]);
  await wsClient.call('sceneLogin', []);
  wsClient.notify('createNpcs', []);
  wsClient.notify('createChars', []);
  useWorldStore.getState().pushChat({
    channelId: 99,
    sender: 'Sistema',
    text: `Bem-vindo ao MCGame Web, ${char.name}! Clique no mapa, nos portais ou aperte M para o Mapa-Múndi.`,
    system: true,
  });
}

export function sendPlayerMove(x: number, y: number, path?: Point2D[]) {
  const rx = Math.max(0, Math.round(x));
  const ry = Math.max(0, Math.round(y));
  const route = path && path.length > 0 ? path : [{ x: rx, y: ry }];
  useWorldStore.getState().updateSelfPosition(rx, ry, false, route);

  if (useSessionStore.getState().offlineDemo || !wsClient.isConnected()) {
    return;
  }

  const now = performance.now();
  if (now - lastMoveSentAt < 620) {
    return;
  }
  lastMoveSentAt = now;
  const serializedRoute = route.slice(0, 24).map((p) => [Math.round(p.x), Math.round(p.y)]);
  wsClient.notify('udcr', [{ x: rx, y: ry, route: serializedRoute }]);
}

export function syncPositionStep(x: number, y: number) {
  if (useSessionStore.getState().offlineDemo || !wsClient.isConnected()) {
    return;
  }
  const now = performance.now();
  if (now - lastMoveSentAt < 650) {
    return;
  }
  lastMoveSentAt = now;
  const rx = Math.max(0, Math.round(x));
  const ry = Math.max(0, Math.round(y));
  wsClient.notify('udcp', [rx, ry, 0, 0, 0]);
}

export async function triggerPortalTransition(portal: PortalItem) {
  const now = performance.now();
  if (now - lastPortalTriggerAt < 1200) return;
  lastPortalTriggerAt = now;

  const session = useSessionStore.getState();
  const world = useWorldStore.getState();
  world.setPendingPortal(null);

  if (session.offlineDemo || !wsClient.isConnected()) {
    startOfflinePreview(portal.destMapId, portal.destX, portal.destY);
    return;
  }

  try {
    await wsClient.call('sceneChange', [portal.id]);
    session.showToast(`Viajando pelo portal: ${portal.name}`);
  } catch (err) {
    session.showToast(err instanceof Error ? err.message : 'Não foi possível usar o portal');
  }
}

export async function teleportToMap(mapId: number, x: number, y: number) {
  const session = useSessionStore.getState();
  const world = useWorldStore.getState();
  world.setWorldMapOpen(false);

  if (session.offlineDemo || !wsClient.isConnected()) {
    startOfflinePreview(mapId, x, y);
    return;
  }

  try {
    await wsClient.call('toMovable', [mapId, Math.round(x), Math.round(y)]);
    session.showToast(`Teleportado para mapa #${mapId}`);
  } catch (err) {
    session.showToast(err instanceof Error ? err.message : 'Teleporte bloqueado (cooldown ou área restrita)');
  }
}

export function sendChatMessage(channelId: number, text: string) {
  const clean = text.trim().slice(0, 200);
  if (!clean) return;

  const self = useWorldStore.getState().self;
  const session = useSessionStore.getState();
  if (session.offlineDemo || !wsClient.isConnected()) {
    useWorldStore.getState().pushChat({
      channelId,
      sender: self?.name ?? 'Aventureiro',
      text: clean,
    });
    return;
  }
  wsClient.notify('say', [self?.id ?? 0, self?.name ?? '', channelId, clean]);
}

export function startOfflinePreview(mapId = 1, customX?: number, customY?: number) {
  const session = useSessionStore.getState();
  const world = useWorldStore.getState();
  const mapInfo = world.mapsCatalog.find((m) => m.id === mapId) ?? world.mapsCatalog[0];
  const sx = customX ?? mapInfo?.safeX ?? 1000;
  const sy = customY ?? mapInfo?.safeY ?? 800;

  session.setAccount('offline-preview', 'PreviewLocal', true);
  session.setConnected(false);
  world.setCurrentMapId(mapInfo?.id ?? mapId);
  world.setSelf({
    id: 900001,
    name: 'Aventureiro',
    x: sx,
    y: sy,
    targetX: sx,
    targetY: sy,
    path: [],
    level: 15,
    classId: 1,
    gender: 0,
    resCode: 2050070100000,
    kind: 'self',
    hp: 420,
    maxHp: 420,
    mp: 280,
    maxMp: 280,
  });
  world.setNpcs({
    1001: {
      id: 1001,
      name: 'Instrutor da Vila',
      x: Math.min((mapInfo?.width ?? 2000) - 80, sx + 180),
      y: Math.max(80, sy - 60),
      targetX: Math.min((mapInfo?.width ?? 2000) - 80, sx + 180),
      targetY: Math.max(80, sy - 60),
      level: 0,
      classId: 0,
      gender: 0,
      resCode: 2060100000001,
      kind: 'npc',
    },
    1002: {
      id: 1002,
      name: 'Comerciante de Poções',
      x: Math.max(80, sx - 210),
      y: Math.min((mapInfo?.height ?? 1500) - 80, sy + 90),
      targetX: Math.max(80, sx - 210),
      targetY: Math.min((mapInfo?.height ?? 1500) - 80, sy + 90),
      level: 0,
      classId: 0,
      gender: 0,
      resCode: 2060100000002,
      kind: 'npc',
    },
  });
  if (world.chatMessages.length === 0) {
    world.pushChat({
      channelId: 99,
      sender: 'Sistema',
      text: 'Preview Local ativo com colisão A*, portais reais e 8 direções! Aperte M para abrir o Mapa-Múndi.',
      system: true,
    });
  }
  session.setScreen('world');
}
