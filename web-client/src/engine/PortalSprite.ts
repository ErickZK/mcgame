import { Container, Graphics, Text, TextStyle } from 'pixi.js';
import type { PortalItem } from '../net/protocol';

export const PORTAL_HALF_W = 91;
export const PORTAL_HALF_H = 68;

export function getPortalCenter(portal: PortalItem): { x: number; y: number } {
  return { x: portal.x + PORTAL_HALF_W, y: portal.y + PORTAL_HALF_H };
}

export class PortalSprite {
  readonly container = new Container();
  readonly portal: PortalItem;
  private outerRing = new Graphics();
  private innerRing = new Graphics();
  private pulseTime = Math.random() * Math.PI * 2;

  constructor(portal: PortalItem, destMapName: string, onClick: (p: PortalItem) => void) {
    this.portal = portal;
    const center = getPortalCenter(portal);
    this.container.position.set(center.x, center.y);
    this.container.zIndex = Math.max(1, center.y - 10);

    const glow = new Graphics();
    glow.ellipse(0, 0, 44, 22).fill({ color: 0x38bdf8, alpha: 0.22 });
    this.container.addChild(glow);

    this.outerRing
      .ellipse(0, 0, 34, 17)
      .stroke({ color: 0x818cf8, width: 3, alpha: 0.9 });
    this.innerRing
      .ellipse(0, 0, 20, 10)
      .fill({ color: 0x22d3ee, alpha: 0.35 })
      .stroke({ color: 0xe0f2fe, width: 2, alpha: 0.95 });

    this.container.addChild(this.outerRing, this.innerRing);

    const labelText = portal.name && portal.name !== 'Tên' ? portal.name : destMapName;
    const label = new Text({
      text: `✦ ${labelText}`,
      style: new TextStyle({
        fontSize: 12,
        fontWeight: '700',
        fill: '#c4b5fd',
        stroke: { color: '#020617', width: 3 },
      }),
    });
    label.anchor.set(0.5, 1);
    label.position.set(0, -24);
    this.container.addChild(label);

    this.container.eventMode = 'static';
    this.container.cursor = 'pointer';
    this.container.on('pointerdown', (e) => {
      e.stopPropagation();
      onClick(this.portal);
    });
  }

  update(dt: number) {
    this.pulseTime += dt * 3;
    const s = 1 + Math.sin(this.pulseTime) * 0.08;
    this.outerRing.scale.set(s);
    this.innerRing.scale.set(1 + Math.cos(this.pulseTime * 1.4) * 0.14);
  }

  destroy() {
    this.container.destroy({ children: true });
  }
}
