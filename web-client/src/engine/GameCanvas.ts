import { Application, Container, Graphics, Sprite } from 'pixi.js';
import { assetManager } from './AssetManager';
import { type CollisionGrid } from './AStarPathfinder';
import { EntitySprite } from './EntitySprite';
import { getPortalCenter, PortalSprite } from './PortalSprite';
import { useWorldStore } from '../store/worldStore';
import { sendPlayerMove, syncPositionStep, triggerPortalTransition } from '../net/gameSession';
import type { Point2D, PortalItem, WorldEntity } from '../net/protocol';

export class GameCanvas {
  private app: Application | null = null;
  private worldContainer = new Container();
  private mapLayer = new Container();
  private pathLayer = new Graphics();
  private portalLayer = new Container();
  private markerLayer = new Container();
  private entityLayer = new Container();
  private clickMarker = new Graphics();
  private clickMarkerLife = 0;

  private entities = new Map<string, EntitySprite>();
  private portals: PortalSprite[] = [];
  private collisionGrid: CollisionGrid | null = null;
  private renderedMapId = -1;
  private mapWidth = 2000;
  private mapHeight = 1500;
  private pressedKeys = new Set<string>();
  private unsubscribeStore: (() => void) | null = null;
  private destroyed = false;

  async mount(host: HTMLElement) {
    const app = new Application();
    await app.init({
      resizeTo: host,
      background: '#020617',
      antialias: true,
      autoDensity: true,
      resolution: Math.min(window.devicePixelRatio || 1, 2),
    });

    if (this.destroyed) {
      app.destroy(true);
      return;
    }

    this.app = app;
    host.innerHTML = '';
    host.appendChild(app.canvas);

    this.entityLayer.sortableChildren = true;
    this.worldContainer.addChild(
      this.mapLayer,
      this.pathLayer,
      this.portalLayer,
      this.markerLayer,
      this.entityLayer,
    );
    this.markerLayer.addChild(this.clickMarker);
    app.stage.addChild(this.worldContainer);

    app.stage.eventMode = 'static';
    app.stage.hitArea = app.screen;
    app.stage.on('pointerdown', (e) => {
      const worldX = e.global.x - this.worldContainer.x;
      const worldY = e.global.y - this.worldContainer.y;
      useWorldStore.getState().setPendingPortal(null);
      this.navigateToPoint(worldX, worldY);
    });

    window.addEventListener('keydown', this.onKeyDown);
    window.addEventListener('keyup', this.onKeyUp);

    this.syncFromStore();
    this.unsubscribeStore = useWorldStore.subscribe(() => this.syncFromStore());

    app.ticker.add((ticker) => {
      const dt = Math.min(0.1, ticker.deltaMS / 1000);
      this.tick(dt);
    });
  }

  private onKeyDown = (e: KeyboardEvent) => {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    const key = e.key.toLowerCase();
    if (key === 'm') {
      const st = useWorldStore.getState();
      st.setWorldMapOpen(!st.worldMapOpen);
      return;
    }
    this.pressedKeys.add(key);
  };

  private onKeyUp = (e: KeyboardEvent) => {
    this.pressedKeys.delete(e.key.toLowerCase());
  };

  private navigateToPoint(rawX: number, rawY: number) {
    const clampedX = Math.max(20, Math.min(this.mapWidth - 20, rawX));
    const clampedY = Math.max(20, Math.min(this.mapHeight - 20, rawY));
    const selfState = useWorldStore.getState().self;
    const selfSprite = selfState ? this.entities.get(`self:${selfState.id}`) : undefined;

    let path: Point2D[] = [{ x: clampedX, y: clampedY }];
    if (this.collisionGrid && selfSprite) {
      const computed = this.collisionGrid.findPath(
        selfSprite.currentX,
        selfSprite.currentY,
        clampedX,
        clampedY,
      );
      if (computed.length > 0) {
        path = computed;
      }
    }

    const finalPt = path[path.length - 1];
    sendPlayerMove(finalPt.x, finalPt.y, path);
    this.showClickMarker(finalPt.x, finalPt.y);
  }

  private handlePortalClick = (portal: PortalItem) => {
    const center = getPortalCenter(portal);
    const selfState = useWorldStore.getState().self;
    const selfSprite = selfState ? this.entities.get(`self:${selfState.id}`) : undefined;
    if (selfSprite && Math.hypot(center.x - selfSprite.currentX, center.y - selfSprite.currentY) <= 110) {
      void triggerPortalTransition(portal);
      return;
    }
    useWorldStore.getState().setPendingPortal(portal);
    this.navigateToPoint(center.x, center.y);
  };

  private showClickMarker(x: number, y: number) {
    this.clickMarker.position.set(x, y);
    this.clickMarkerLife = 0.65;
  }

  private syncFromStore() {
    const state = useWorldStore.getState();

    if (state.currentMapId !== this.renderedMapId) {
      this.renderedMapId = state.currentMapId;
      void this.loadMap(state.currentMapId);
    }

    if (state.pendingNavTarget) {
      const target = state.pendingNavTarget;
      state.requestNavigateTo(null);
      this.navigateToPoint(target.x, target.y);
    }

    const desiredKeys = new Set<string>();
    const allEntities: WorldEntity[] = [];
    if (state.self) allEntities.push(state.self);
    for (const p of Object.values(state.players)) allEntities.push(p);
    for (const n of Object.values(state.npcs)) allEntities.push(n);

    for (const ent of allEntities) {
      const key = `${ent.kind}:${ent.id}`;
      desiredKeys.add(key);
      let sprite = this.entities.get(key);
      if (!sprite) {
        sprite = new EntitySprite(ent, (npc) => {
          useWorldStore.getState().setSelectedNpc(npc);
        });
        this.entities.set(key, sprite);
        this.entityLayer.addChild(sprite.container);
      } else {
        sprite.syncData(ent);
      }
    }

    for (const [key, sprite] of this.entities.entries()) {
      if (!desiredKeys.has(key)) {
        this.entityLayer.removeChild(sprite.container);
        sprite.destroy();
        this.entities.delete(key);
      }
    }
  }

  private async loadMap(mapId: number) {
    await assetManager.ensureCatalogs();
    const info = assetManager.getMapInfo(mapId);
    this.mapWidth = info?.width ?? 2000;
    this.mapHeight = info?.height ?? 1500;

    this.mapLayer.removeChildren();
    for (const p of this.portals) {
      this.portalLayer.removeChild(p.container);
      p.destroy();
    }
    this.portals = [];

    const [tex, grid] = await Promise.all([
      assetManager.loadMapTexture(mapId),
      assetManager.loadCollisionGrid(mapId),
    ]);
    if (this.destroyed || this.renderedMapId !== mapId) return;

    this.collisionGrid = grid;

    if (tex) {
      const bg = new Sprite(tex);
      bg.width = this.mapWidth;
      bg.height = this.mapHeight;
      this.mapLayer.addChild(bg);
    } else {
      const fallback = new Graphics();
      fallback.rect(0, 0, this.mapWidth, this.mapHeight).fill(0x0f172a);
      for (let x = 0; x <= this.mapWidth; x += 100) {
        fallback.moveTo(x, 0).lineTo(x, this.mapHeight).stroke({ color: 0x1e293b, width: 1 });
      }
      for (let y = 0; y <= this.mapHeight; y += 100) {
        fallback.moveTo(0, y).lineTo(this.mapWidth, y).stroke({ color: 0x1e293b, width: 1 });
      }
      this.mapLayer.addChild(fallback);
    }

    if (info?.portals) {
      for (const portal of info.portals) {
        const destName = assetManager.getMapInfo(portal.destMapId)?.name ?? `Mapa #${portal.destMapId}`;
        const ps = new PortalSprite(portal, destName, this.handlePortalClick);
        this.portals.push(ps);
        this.portalLayer.addChild(ps.container);
      }
    }
  }

  private tick(dt: number) {
    if (!this.app) return;

    const state = useWorldStore.getState();
    const selfState = state.self;
    const selfSprite = selfState ? this.entities.get(`self:${selfState.id}`) : undefined;

    if (selfState && selfSprite) {
      let vx = 0;
      let vy = 0;
      if (this.pressedKeys.has('w') || this.pressedKeys.has('arrowup')) vy -= 1;
      if (this.pressedKeys.has('s') || this.pressedKeys.has('arrowdown')) vy += 1;
      if (this.pressedKeys.has('a') || this.pressedKeys.has('arrowleft')) vx -= 1;
      if (this.pressedKeys.has('d') || this.pressedKeys.has('arrowright')) vx += 1;

      if (vx !== 0 || vy !== 0) {
        const len = Math.hypot(vx, vy);
        const nx = Math.max(20, Math.min(this.mapWidth - 20, selfSprite.currentX + (vx / len) * 240 * dt));
        const ny = Math.max(20, Math.min(this.mapHeight - 20, selfSprite.currentY + (vy / len) * 240 * dt));
        if (!this.collisionGrid || this.collisionGrid.isPixelWalkable(nx, ny)) {
          sendPlayerMove(nx, ny, [{ x: nx, y: ny }]);
        } else if (this.collisionGrid.isPixelWalkable(nx, selfSprite.currentY)) {
          sendPlayerMove(nx, selfSprite.currentY, [{ x: nx, y: selfSprite.currentY }]);
        } else if (this.collisionGrid.isPixelWalkable(selfSprite.currentX, ny)) {
          sendPlayerMove(selfSprite.currentX, ny, [{ x: selfSprite.currentX, y: ny }]);
        }
      }
    }

    for (const ps of this.portals) {
      ps.update(dt);
    }

    let selfMoved = false;
    for (const [key, sprite] of this.entities.entries()) {
      const moved = sprite.update(dt);
      if (selfState && key === `self:${selfState.id}`) {
        selfMoved = moved;
      }
    }

    this.pathLayer.clear();
    if (selfSprite && selfSprite.waypoints.length > 0) {
      this.pathLayer.moveTo(selfSprite.currentX, selfSprite.currentY);
      for (const wp of selfSprite.waypoints) {
        this.pathLayer.lineTo(wp.x, wp.y);
      }
      this.pathLayer.stroke({ color: 0x38bdf8, width: 2, alpha: 0.45 });
    }

    if (selfSprite && selfState) {
      if (selfMoved) {
        syncPositionStep(selfSprite.currentX, selfSprite.currentY);
      }
      if (state.pendingPortal) {
        const center = getPortalCenter(state.pendingPortal);
        const d = Math.hypot(
          center.x - selfSprite.currentX,
          center.y - selfSprite.currentY,
        );
        if (d <= 95) {
          void triggerPortalTransition(state.pendingPortal);
        }
      }
    }

    if (this.clickMarkerLife > 0) {
      this.clickMarkerLife = Math.max(0, this.clickMarkerLife - dt);
      const t = 1 - this.clickMarkerLife / 0.65;
      this.clickMarker.clear();
      this.clickMarker
        .ellipse(0, 0, 10 + t * 22, 5 + t * 10)
        .stroke({ color: 0x38bdf8, width: 2, alpha: 1 - t });
    } else {
      this.clickMarker.clear();
    }

    if (selfSprite) {
      const viewW = this.app.screen.width;
      const viewH = this.app.screen.height;
      const desiredX = viewW / 2 - selfSprite.currentX;
      const desiredY = viewH / 2 - selfSprite.currentY;

      const minX = Math.min(0, viewW - this.mapWidth);
      const minY = Math.min(0, viewH - this.mapHeight);
      const clampedCamX = Math.max(minX, Math.min(0, desiredX));
      const clampedCamY = Math.max(minY, Math.min(0, desiredY));

      this.worldContainer.x += (clampedCamX - this.worldContainer.x) * Math.min(1, dt * 8);
      this.worldContainer.y += (clampedCamY - this.worldContainer.y) * Math.min(1, dt * 8);
    }
  }

  destroy() {
    this.destroyed = true;
    window.removeEventListener('keydown', this.onKeyDown);
    window.removeEventListener('keyup', this.onKeyUp);
    this.unsubscribeStore?.();
    this.unsubscribeStore = null;
    for (const s of this.entities.values()) s.destroy();
    this.entities.clear();
    for (const p of this.portals) p.destroy();
    this.portals = [];
    if (this.app) {
      this.app.destroy(true);
      this.app = null;
    }
  }
}
