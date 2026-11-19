import { z } from 'zod';

export const LoginResponseSchema = z.object({
  ticket: z.string().min(1),
  expiresAt: z.string(),
  accountId: z.string(),
  username: z.string(),
});

export type LoginResponse = z.infer<typeof LoginResponseSchema>;

export const OutgoingEnvelopeSchema = z.object({
  op: z.enum(['res', 'err', 'evt']),
  tx: z.number().optional(),
  m: z.string().optional(),
  a: z.array(z.unknown()).optional(),
  d: z.unknown().optional(),
  e: z.string().optional(),
});

export type ServerEnvelope = z.infer<typeof OutgoingEnvelopeSchema>;

export interface CharacterSummary {
  id: number;
  name: string;
  level: number;
  exp: number;
  classId: number;
  gender: number;
  iconCode: number;
}

export interface Point2D {
  x: number;
  y: number;
}

export interface WorldEntity {
  id: number;
  name: string;
  x: number;
  y: number;
  targetX: number;
  targetY: number;
  path?: Point2D[];
  level: number;
  classId: number;
  gender: number;
  resCode: number;
  kind: 'self' | 'player' | 'npc';
  hp?: number;
  maxHp?: number;
  mp?: number;
  maxMp?: number;
  bubbleText?: string;
  bubbleUntil?: number;
}

export interface PortalItem {
  id: number;
  name: string;
  x: number;
  y: number;
  destMapId: number;
  destX: number;
  destY: number;
}

export interface MapCatalogItem {
  id: number;
  name: string;
  width: number;
  height: number;
  safeX: number;
  safeY: number;
  resCode: number;
  image: string;
  collisionBin?: string;
  gridStep?: number;
  portals?: PortalItem[];
}

export interface SpriteCatalogItem {
  key: string;
  resCode?: number;
  category: string;
  sheetJson: string;
  sheetImage: string;
  frameCount: number;
  frameWidth: number;
  frameHeight: number;
  animations?: string[];
}

export interface ChatMessage {
  id: string;
  channelId: number;
  sender: string;
  text: string;
  timestamp: number;
  system?: boolean;
}

export const GAME_CLASSES = [
  { id: 1, name: 'Mago Elemental', role: 'Dano Mágico em Área', color: '#60a5fa' },
  { id: 2, name: 'Guerreiro Arcano', role: 'Combate Corpo-a-Corpo', color: '#f87171' },
  { id: 3, name: 'Sacerdote da Luz', role: 'Cura e Suporte', color: '#fbbf24' },
  { id: 4, name: 'Atirador Real', role: 'Precisão à Distância', color: '#34d399' },
  { id: 5, name: 'Caçador Sombrio', role: 'Crítico e Agilidade', color: '#c084fc' },
  { id: 6, name: 'Bardo Místico', role: 'Buffs e Controle', color: '#f472b6' },
] as const;

export function getClassInfo(classId: number) {
  return GAME_CLASSES.find((c) => c.id === classId) ?? GAME_CLASSES[0];
}
