import { Assets, Spritesheet, Texture } from 'pixi.js';
import type { MapCatalogItem, SpriteCatalogItem } from '../net/protocol';
import { CollisionGrid } from './AStarPathfinder';

const ASSET_CATALOG_VERSION = '20261119-b2';

class AssetManager {
  private maps: MapCatalogItem[] = [];
  private sprites: SpriteCatalogItem[] = [];
  private spriteByResCode = new Map<number, SpriteCatalogItem>();
  private spriteByKey = new Map<string, SpriteCatalogItem>();
  private loadedCatalogs = false;
  private sheetCache = new Map<string, Spritesheet | null>();
  private mapTexCache = new Map<number, Texture | null>();
  private collisionCache = new Map<number, CollisionGrid | null>();

  async ensureCatalogs(): Promise<{ maps: MapCatalogItem[]; sprites: SpriteCatalogItem[] }> {
    if (this.loadedCatalogs) {
      return { maps: this.maps, sprites: this.sprites };
    }
    const [mapsRes, spritesRes] = await Promise.allSettled([
      fetch(`/assets/maps/index.json?v=${ASSET_CATALOG_VERSION}`, { cache: 'no-cache' }).then((r) =>
        r.ok ? r.json() : [],
      ),
      fetch(`/assets/sprites/index.json?v=${ASSET_CATALOG_VERSION}`, { cache: 'no-cache' }).then((r) =>
        r.ok ? r.json() : [],
      ),
    ]);
    if (mapsRes.status === 'fulfilled' && Array.isArray(mapsRes.value)) {
      this.maps = mapsRes.value as MapCatalogItem[];
    }
    if (spritesRes.status === 'fulfilled' && Array.isArray(spritesRes.value)) {
      this.sprites = spritesRes.value as SpriteCatalogItem[];
      for (const item of this.sprites) {
        this.spriteByKey.set(item.key, item);
        if (item.resCode && item.resCode > 0) {
          this.spriteByResCode.set(item.resCode, item);
        }
      }
    }
    this.loadedCatalogs = true;
    return { maps: this.maps, sprites: this.sprites };
  }

  getMapInfo(mapId: number): MapCatalogItem | undefined {
    return this.maps.find((m) => m.id === mapId);
  }

  async loadMapTexture(mapId: number): Promise<Texture | null> {
    if (this.mapTexCache.has(mapId)) {
      return this.mapTexCache.get(mapId) ?? null;
    }
    await this.ensureCatalogs();
    const info = this.getMapInfo(mapId);
    if (!info) {
      this.mapTexCache.set(mapId, null);
      return null;
    }
    try {
      const tex = await Assets.load<Texture>(info.image);
      this.mapTexCache.set(mapId, tex);
      return tex;
    } catch {
      this.mapTexCache.set(mapId, null);
      return null;
    }
  }

  async loadCollisionGrid(mapId: number): Promise<CollisionGrid | null> {
    if (this.collisionCache.has(mapId)) {
      return this.collisionCache.get(mapId) ?? null;
    }
    await this.ensureCatalogs();
    const info = this.getMapInfo(mapId);
    if (!info?.collisionBin) {
      this.collisionCache.set(mapId, null);
      return null;
    }
    try {
      const res = await fetch(`${info.collisionBin}?v=${ASSET_CATALOG_VERSION}`, { cache: 'no-cache' });
      if (!res.ok) {
        this.collisionCache.set(mapId, null);
        return null;
      }
      const buf = await res.arrayBuffer();
      const grid = CollisionGrid.fromBinary(buf, info.gridStep ?? 20);
      this.collisionCache.set(mapId, grid);
      return grid;
    } catch {
      this.collisionCache.set(mapId, null);
      return null;
    }
  }

  pickSpriteEntry(
    kind: 'self' | 'player' | 'npc',
    resCode: number,
    classId: number,
    gender: number,
    id: number,
  ): SpriteCatalogItem | null {
    if (resCode > 0) {
      const exact = this.spriteByResCode.get(resCode);
      if (exact) return exact;
    }

    if (kind !== 'npc') {
      const prefix = gender === 1 ? 'PC_FEMALE_' : 'PC_MALE_';
      const byClass = this.spriteByKey.get(`${prefix}${String(classId).padStart(6, '0')}`);
      if (byClass) return byClass;
      const pool = this.sprites.filter((s) => s.key.startsWith(prefix));
      if (pool.length > 0) {
        return pool[Math.abs(classId) % pool.length];
      }
    }

    const pool = this.sprites.filter((s) => (kind === 'npc' ? s.category === 'npc' : s.category === 'pc'));
    if (pool.length === 0) return null;
    return pool[Math.abs(id) % pool.length];
  }

  async loadSpritesheet(entry: SpriteCatalogItem): Promise<Spritesheet | null> {
    if (this.sheetCache.has(entry.key)) {
      return this.sheetCache.get(entry.key) ?? null;
    }
    try {
      const sheet = await Assets.load<Spritesheet>(`${entry.sheetJson}?v=${ASSET_CATALOG_VERSION}`);
      this.sheetCache.set(entry.key, sheet);
      return sheet;
    } catch {
      this.sheetCache.set(entry.key, null);
      return null;
    }
  }
}

export const assetManager = new AssetManager();
