import { AnimatedSprite, Container, Graphics, Spritesheet, Text, TextStyle, Texture } from 'pixi.js';
import { getClassInfo, type Point2D, type WorldEntity } from '../net/protocol';
import { assetManager } from './AssetManager';

interface DirMapping {
  baseDir: number;
  flipX: boolean;
}

function vectorToDir8(dx: number, dy: number): DirMapping {
  const deg = (Math.atan2(dy, dx) * 180) / Math.PI;
  if (deg >= 67.5 && deg < 112.5) return { baseDir: 0, flipX: false };
  if (deg >= 112.5 && deg < 157.5) return { baseDir: 2, flipX: false };
  if (deg >= 157.5 || deg < -157.5) return { baseDir: 1, flipX: false };
  if (deg >= -157.5 && deg < -112.5) return { baseDir: 3, flipX: false };
  if (deg >= -112.5 && deg < -67.5) return { baseDir: 4, flipX: false };
  if (deg >= -67.5 && deg < -22.5) return { baseDir: 3, flipX: true };
  if (deg >= -22.5 && deg < 22.5) return { baseDir: 1, flipX: true };
  return { baseDir: 2, flipX: true };
}

export class EntitySprite {
  readonly container = new Container();
  private bodyLayer = new Container();
  private nameLabel: Text;
  private bubbleContainer = new Container();
  private bubbleBg = new Graphics();
  private bubbleLabel: Text;
  private animSprite: AnimatedSprite | null = null;
  private sheet: Spritesheet | null = null;
  private baseScale = 1;
  private currentAnimKey = '';
  private spriteLoaded = false;

  currentX: number;
  currentY: number;
  targetX: number;
  targetY: number;
  waypoints: Point2D[] = [];
  facing: DirMapping = { baseDir: 0, flipX: false };
  isMoving = false;
  entity: WorldEntity;

  constructor(entity: WorldEntity, onNpcClick?: (npc: WorldEntity) => void) {
    this.entity = entity;
    this.currentX = entity.x;
    this.currentY = entity.y;
    this.targetX = entity.targetX;
    this.targetY = entity.targetY;
    if (entity.path && entity.path.length > 0) {
      this.waypoints = [...entity.path];
    }

    this.container.position.set(entity.x, entity.y);
    this.container.zIndex = entity.y;

    const shadow = new Graphics();
    shadow.ellipse(0, 0, 24, 10).fill({ color: 0x000000, alpha: 0.42 });
    this.container.addChild(shadow);

    this.container.addChild(this.bodyLayer);
    this.renderFallbackAvatar();

    const labelColor =
      entity.kind === 'self' ? '#4ade80' : entity.kind === 'npc' ? '#fbbf24' : '#7dd3fc';

    this.nameLabel = new Text({
      text: entity.kind === 'npc' ? `[NPC] ${entity.name}` : entity.name,
      style: new TextStyle({
        fontSize: 13,
        fontWeight: '700',
        fill: labelColor,
        stroke: { color: '#020617', width: 3 },
      }),
    });
    this.nameLabel.anchor.set(0.5, 0);
    this.nameLabel.position.set(0, 10);
    this.container.addChild(this.nameLabel);

    this.bubbleLabel = new Text({
      text: '',
      style: new TextStyle({
        fontSize: 12,
        fill: '#f8fafc',
        wordWrap: true,
        wordWrapWidth: 180,
      }),
    });
    this.bubbleLabel.anchor.set(0.5, 1);
    this.bubbleContainer.addChild(this.bubbleBg, this.bubbleLabel);
    this.bubbleContainer.visible = false;
    this.bubbleContainer.position.set(0, -112);
    this.container.addChild(this.bubbleContainer);

    if (entity.kind === 'npc' && onNpcClick) {
      this.container.eventMode = 'static';
      this.container.cursor = 'pointer';
      this.container.on('pointerdown', (e) => {
        e.stopPropagation();
        onNpcClick(this.entity);
      });
    }

    void this.loadRealSprite();
  }

  private renderFallbackAvatar() {
    this.bodyLayer.removeChildren();
    const g = new Graphics();
    const colorHex =
      this.entity.kind === 'npc'
        ? '#f59e0b'
        : getClassInfo(this.entity.classId).color;
    const color = parseInt(colorHex.replace('#', ''), 16);

    g.roundRect(-16, -54, 32, 48, 12).fill({ color, alpha: 0.92 }).stroke({ color: 0xffffff, width: 2 });
    g.circle(0, -68, 15).fill({ color: 0xfde68a }).stroke({ color: 0x1e293b, width: 2 });
    this.bodyLayer.addChild(g);
  }

  private async loadRealSprite() {
    if (this.spriteLoaded) return;
    this.spriteLoaded = true;
    await assetManager.ensureCatalogs();
    const entry = assetManager.pickSpriteEntry(
      this.entity.kind,
      this.entity.resCode,
      this.entity.classId,
      this.entity.gender,
      this.entity.id,
    );
    if (!entry) return;

    const sheet = await assetManager.loadSpritesheet(entry);
    if (!sheet) return;
    this.sheet = sheet;

    const initialFrames = this.resolveFrames(0, false);
    if (!initialFrames || initialFrames.length === 0) return;

    const maxVisualH = 260;
    this.baseScale = entry.frameHeight > maxVisualH ? maxVisualH / entry.frameHeight : 1;

    const anim = new AnimatedSprite(initialFrames);
    anim.anchor.set(0.5, 0.98);
    anim.animationSpeed = 0.14;
    anim.scale.set(this.baseScale);
    anim.play();

    this.bodyLayer.removeChildren();
    this.bodyLayer.addChild(anim);
    this.animSprite = anim;
    this.applyAnimation(this.facing, this.isMoving);
  }

  private resolveFrames(baseDir: number, moving: boolean): Texture[] | null {
    if (!this.sheet?.animations) return null;
    const anims = this.sheet.animations;
    const candidates = moving
      ? [`${baseDir}-1`, `${baseDir}-2`, `${baseDir}-0`, '0-1', '0-2', '0-0', 'idle']
      : [`${baseDir}-0`, '0-0', 'idle'];
    for (const key of candidates) {
      const frames = anims[key];
      if (frames && frames.length > 0) {
        return frames;
      }
    }
    return null;
  }

  private applyAnimation(dir: DirMapping, moving: boolean) {
    if (!this.animSprite || !this.sheet) return;
    const desiredKey = `${dir.baseDir}:${moving ? 1 : 0}`;
    if (desiredKey !== this.currentAnimKey) {
      const frames = this.resolveFrames(dir.baseDir, moving);
      if (frames && frames.length > 0) {
        this.animSprite.textures = frames;
        this.animSprite.animationSpeed = moving ? 0.22 : 0.12;
        this.animSprite.play();
      }
      this.currentAnimKey = desiredKey;
    }
    this.animSprite.scale.x = dir.flipX ? -this.baseScale : this.baseScale;
    this.animSprite.scale.y = this.baseScale;
  }

  syncData(entity: WorldEntity) {
    const posJumped =
      Math.hypot(entity.x - this.entity.x, entity.y - this.entity.y) > 400 &&
      (!entity.path || entity.path.length === 0);

    this.entity = entity;
    if (posJumped) {
      this.currentX = entity.x;
      this.currentY = entity.y;
      this.waypoints = [];
    }
    this.targetX = entity.targetX;
    this.targetY = entity.targetY;
    if (entity.path && entity.path.length > 0) {
      this.waypoints = [...entity.path];
    } else if (Math.hypot(this.targetX - this.currentX, this.targetY - this.currentY) > 4) {
      this.waypoints = [{ x: this.targetX, y: this.targetY }];
    }

    const now = Date.now();
    if (entity.bubbleText && entity.bubbleUntil && entity.bubbleUntil > now) {
      this.bubbleLabel.text = entity.bubbleText;
      const padX = 10;
      const padY = 6;
      const w = Math.max(40, this.bubbleLabel.width + padX * 2);
      const h = this.bubbleLabel.height + padY * 2;
      this.bubbleBg.clear();
      this.bubbleBg
        .roundRect(-w / 2, -h, w, h, 8)
        .fill({ color: 0x0f172a, alpha: 0.88 })
        .stroke({ color: 0x38bdf8, width: 1.5 });
      this.bubbleLabel.position.set(0, -padY);
      this.bubbleContainer.visible = true;
    } else {
      this.bubbleContainer.visible = false;
    }
  }

  update(dtSeconds: number): boolean {
    const speed = 260;
    let remaining = speed * dtSeconds;
    let moved = false;

    while (this.waypoints.length > 0 && remaining > 0) {
      const next = this.waypoints[0];
      const dx = next.x - this.currentX;
      const dy = next.y - this.currentY;
      const dist = Math.hypot(dx, dy);

      if (dist <= 2) {
        this.currentX = next.x;
        this.currentY = next.y;
        this.waypoints.shift();
        continue;
      }

      moved = true;
      this.facing = vectorToDir8(dx, dy);
      if (dist <= remaining) {
        this.currentX = next.x;
        this.currentY = next.y;
        remaining -= dist;
        this.waypoints.shift();
      } else {
        this.currentX += (dx / dist) * remaining;
        this.currentY += (dy / dist) * remaining;
        remaining = 0;
      }
    }

    this.isMoving = moved;
    this.applyAnimation(this.facing, this.isMoving);

    this.container.position.set(this.currentX, this.currentY);
    this.container.zIndex = Math.round(this.currentY);

    if (this.bubbleContainer.visible && this.entity.bubbleUntil && Date.now() > this.entity.bubbleUntil) {
      this.bubbleContainer.visible = false;
    }
    return moved;
  }

  destroy() {
    this.container.destroy({ children: true });
  }
}
