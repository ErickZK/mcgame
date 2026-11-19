import { create } from 'zustand';
import type { ChatMessage, MapCatalogItem, Point2D, PortalItem, WorldEntity } from '../net/protocol';

interface WorldState {
  mapsCatalog: MapCatalogItem[];
  currentMapId: number;
  self: WorldEntity | null;
  players: Record<number, WorldEntity>;
  npcs: Record<number, WorldEntity>;
  chatMessages: ChatMessage[];
  selectedNpc: WorldEntity | null;
  worldMapOpen: boolean;
  pendingNavTarget: Point2D | null;
  pendingPortal: PortalItem | null;

  setMapsCatalog: (maps: MapCatalogItem[]) => void;
  setCurrentMapId: (mapId: number) => void;
  setSelf: (entity: WorldEntity | null) => void;
  updateSelfPosition: (x: number, y: number, immediate?: boolean, path?: Point2D[]) => void;
  upsertPlayer: (entity: WorldEntity) => void;
  removePlayer: (id: number) => void;
  moveEntity: (id: number, x: number, y: number, path?: Point2D[]) => void;
  setNpcs: (npcs: Record<number, WorldEntity>) => void;
  setSelectedNpc: (npc: WorldEntity | null) => void;
  setWorldMapOpen: (open: boolean) => void;
  requestNavigateTo: (pt: Point2D | null) => void;
  setPendingPortal: (portal: PortalItem | null) => void;
  pushChat: (msg: Omit<ChatMessage, 'id' | 'timestamp'>) => void;
  resetWorld: () => void;
}

let chatSeq = 1;

export const useWorldStore = create<WorldState>((set) => ({
  mapsCatalog: [],
  currentMapId: 1,
  self: null,
  players: {},
  npcs: {},
  chatMessages: [],
  selectedNpc: null,
  worldMapOpen: false,
  pendingNavTarget: null,
  pendingPortal: null,

  setMapsCatalog: (mapsCatalog) => set({ mapsCatalog }),
  setCurrentMapId: (currentMapId) =>
    set({
      currentMapId,
      players: {},
      npcs: {},
      selectedNpc: null,
      pendingNavTarget: null,
      pendingPortal: null,
    }),
  setSelf: (self) => set({ self }),
  updateSelfPosition: (x, y, immediate = false, path) =>
    set((state) => {
      if (!state.self) return state;
      return {
        self: {
          ...state.self,
          x: immediate ? x : state.self.x,
          y: immediate ? y : state.self.y,
          targetX: x,
          targetY: y,
          path: immediate ? [] : (path ?? state.self.path),
        },
      };
    }),
  upsertPlayer: (entity) =>
    set((state) => ({
      players: { ...state.players, [entity.id]: entity },
    })),
  removePlayer: (id) =>
    set((state) => {
      const next = { ...state.players };
      delete next[id];
      return { players: next };
    }),
  moveEntity: (id, x, y, path) =>
    set((state) => {
      if (state.self && state.self.id === id) {
        return {
          self: { ...state.self, targetX: x, targetY: y, path: path ?? [{ x, y }] },
        };
      }
      const existing = state.players[id];
      if (!existing) return state;
      return {
        players: {
          ...state.players,
          [id]: { ...existing, targetX: x, targetY: y, path: path ?? [{ x, y }] },
        },
      };
    }),
  setNpcs: (npcs) => set({ npcs }),
  setSelectedNpc: (selectedNpc) => set({ selectedNpc }),
  setWorldMapOpen: (worldMapOpen) => set({ worldMapOpen }),
  requestNavigateTo: (pendingNavTarget) => set({ pendingNavTarget }),
  setPendingPortal: (pendingPortal) => set({ pendingPortal }),
  pushChat: (msg) =>
    set((state) => {
      const next: ChatMessage = {
        ...msg,
        id: `m-${chatSeq++}`,
        timestamp: Date.now(),
      };
      const trimmed = [...state.chatMessages.slice(-119), next];
      let self = state.self;
      const players = { ...state.players };
      if (!msg.system) {
        if (self && self.name === msg.sender) {
          self = { ...self, bubbleText: msg.text, bubbleUntil: Date.now() + 5000 };
        } else {
          for (const key of Object.keys(players)) {
            const p = players[Number(key)];
            if (p && p.name === msg.sender) {
              players[Number(key)] = { ...p, bubbleText: msg.text, bubbleUntil: Date.now() + 5000 };
            }
          }
        }
      }
      return { chatMessages: trimmed, self, players };
    }),
  resetWorld: () =>
    set({
      currentMapId: 1,
      self: null,
      players: {},
      npcs: {},
      chatMessages: [],
      selectedNpc: null,
      worldMapOpen: false,
      pendingNavTarget: null,
      pendingPortal: null,
    }),
}));
