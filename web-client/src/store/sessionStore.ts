import { create } from 'zustand';
import type { CharacterSummary } from '../net/protocol';

export type ScreenMode = 'login' | 'charSelect' | 'world';

interface SessionState {
  screen: ScreenMode;
  offlineDemo: boolean;
  connected: boolean;
  accountId: string;
  username: string;
  latencyMs: number;
  characters: CharacterSummary[];
  toast: string | null;
  setScreen: (screen: ScreenMode) => void;
  setAccount: (accountId: string, username: string, offlineDemo?: boolean) => void;
  setConnected: (connected: boolean) => void;
  setLatency: (ms: number) => void;
  setCharacters: (chars: CharacterSummary[]) => void;
  showToast: (msg: string | null) => void;
  resetSession: () => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  screen: 'login',
  offlineDemo: false,
  connected: false,
  accountId: '',
  username: '',
  latencyMs: 0,
  characters: [],
  toast: null,

  setScreen: (screen) => set({ screen }),
  setAccount: (accountId, username, offlineDemo = false) =>
    set({ accountId, username, offlineDemo }),
  setConnected: (connected) => set({ connected }),
  setLatency: (latencyMs) => set({ latencyMs }),
  setCharacters: (characters) => set({ characters }),
  showToast: (toast) => set({ toast }),
  resetSession: () =>
    set({
      screen: 'login',
      offlineDemo: false,
      connected: false,
      accountId: '',
      username: '',
      latencyMs: 0,
      characters: [],
      toast: null,
    }),
}));
