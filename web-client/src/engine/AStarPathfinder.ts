import type { Point2D } from '../net/protocol';

export class CollisionGrid {
  readonly cols: number;
  readonly rows: number;
  readonly step: number;
  private readonly bits: Uint8Array;

  constructor(cols: number, rows: number, step: number, bits: Uint8Array) {
    this.cols = cols;
    this.rows = rows;
    this.step = step;
    this.bits = bits;
  }

  static fromBinary(buffer: ArrayBuffer, step = 20): CollisionGrid | null {
    if (buffer.byteLength < 5) return null;
    const view = new DataView(buffer);
    const cols = view.getUint16(0, true);
    const rows = view.getUint16(2, true);
    const bits = new Uint8Array(buffer, 4);
    if (cols === 0 || rows === 0 || bits.length < Math.ceil((cols * rows) / 8)) {
      return null;
    }
    return new CollisionGrid(cols, rows, step, bits);
  }

  isCellWalkable(gx: number, gy: number): boolean {
    if (gx < 0 || gy < 0 || gx >= this.cols || gy >= this.rows) return false;
    const idx = gy * this.cols + gx;
    return (this.bits[idx >> 3] & (1 << (idx & 7))) !== 0;
  }

  isPixelWalkable(px: number, py: number): boolean {
    const gx = Math.floor(px / this.step);
    const gy = Math.floor(py / this.step);
    return this.isCellWalkable(gx, gy);
  }

  nearestWalkableCell(gx: number, gy: number, maxRadius = 14): { gx: number; gy: number } | null {
    const cx = Math.max(0, Math.min(this.cols - 1, gx));
    const cy = Math.max(0, Math.min(this.rows - 1, gy));
    if (this.isCellWalkable(cx, cy)) return { gx: cx, gy: cy };

    for (let r = 1; r <= maxRadius; r++) {
      for (let dy = -r; dy <= r; dy++) {
        for (let dx = -r; dx <= r; dx++) {
          if (Math.abs(dx) !== r && Math.abs(dy) !== r) continue;
          const nx = cx + dx;
          const ny = cy + dy;
          if (this.isCellWalkable(nx, ny)) {
            return { gx: nx, gy: ny };
          }
        }
      }
    }
    return null;
  }

  findPath(startX: number, startY: number, goalX: number, goalY: number, maxNodes = 6000): Point2D[] {
    const startCell = this.nearestWalkableCell(
      Math.floor(startX / this.step),
      Math.floor(startY / this.step),
      8,
    );
    const goalCell = this.nearestWalkableCell(
      Math.floor(goalX / this.step),
      Math.floor(goalY / this.step),
      16,
    );
    if (!startCell || !goalCell) {
      return [{ x: goalX, y: goalY }];
    }
    if (startCell.gx === goalCell.gx && startCell.gy === goalCell.gy) {
      return [
        {
          x: goalCell.gx * this.step + this.step / 2,
          y: goalCell.gy * this.step + this.step / 2,
        },
      ];
    }

    const total = this.cols * this.rows;
    const gScore = new Int32Array(total).fill(1e9);
    const fScore = new Int32Array(total).fill(1e9);
    const parent = new Int32Array(total).fill(-1);
    const closed = new Uint8Array(total);
    const inOpen = new Uint8Array(total);

    const startIdx = startCell.gy * this.cols + startCell.gx;
    const goalIdx = goalCell.gy * this.cols + goalCell.gx;

    const heap: number[] = [startIdx];
    gScore[startIdx] = 0;
    fScore[startIdx] = octileHeuristic(startCell.gx, startCell.gy, goalCell.gx, goalCell.gy);
    inOpen[startIdx] = 1;

    const pushHeap = (idx: number) => {
      heap.push(idx);
      let i = heap.length - 1;
      while (i > 0) {
        const p = (i - 1) >> 1;
        if (fScore[heap[p]] <= fScore[heap[i]]) break;
        const tmp = heap[p];
        heap[p] = heap[i];
        heap[i] = tmp;
        i = p;
      }
    };

    const popHeap = (): number => {
      const top = heap[0];
      const last = heap.pop()!;
      if (heap.length > 0) {
        heap[0] = last;
        let i = 0;
        const len = heap.length;
        while (true) {
          const l = i * 2 + 1;
          const r = l + 1;
          let smallest = i;
          if (l < len && fScore[heap[l]] < fScore[heap[smallest]]) smallest = l;
          if (r < len && fScore[heap[r]] < fScore[heap[smallest]]) smallest = r;
          if (smallest === i) break;
          const tmp = heap[i];
          heap[i] = heap[smallest];
          heap[smallest] = tmp;
          i = smallest;
        }
      }
      return top;
    };

    const dirs = [
      [1, 0, 10],
      [-1, 0, 10],
      [0, 1, 10],
      [0, -1, 10],
      [1, 1, 14],
      [-1, 1, 14],
      [-1, -1, 14],
      [1, -1, 14],
    ];

    let visited = 0;
    let bestFallback = startIdx;
    let bestH = fScore[startIdx];

    while (heap.length > 0 && visited < maxNodes) {
      const cur = popHeap();
      if (closed[cur]) continue;
      closed[cur] = 1;
      visited++;

      if (cur === goalIdx) {
        bestFallback = cur;
        break;
      }

      const cx = cur % this.cols;
      const cy = Math.floor(cur / this.cols);
      const curH = octileHeuristic(cx, cy, goalCell.gx, goalCell.gy);
      if (curH < bestH) {
        bestH = curH;
        bestFallback = cur;
      }

      for (const [dx, dy, cost] of dirs) {
        const nx = cx + dx;
        const ny = cy + dy;
        if (!this.isCellWalkable(nx, ny)) continue;
        if (dx !== 0 && dy !== 0) {
          if (!this.isCellWalkable(cx + dx, cy) || !this.isCellWalkable(cx, cy + dy)) {
            continue;
          }
        }
        const nIdx = ny * this.cols + nx;
        if (closed[nIdx]) continue;

        const tentativeG = gScore[cur] + cost;
        if (tentativeG < gScore[nIdx]) {
          parent[nIdx] = cur;
          gScore[nIdx] = tentativeG;
          fScore[nIdx] = tentativeG + octileHeuristic(nx, ny, goalCell.gx, goalCell.gy);
          if (!inOpen[nIdx]) {
            inOpen[nIdx] = 1;
            pushHeap(nIdx);
          } else {
            pushHeap(nIdx);
          }
        }
      }
    }

    const rawPath: Point2D[] = [];
    let trace = bestFallback;
    while (trace !== -1 && trace !== startIdx) {
      const gx = trace % this.cols;
      const gy = Math.floor(trace / this.cols);
      rawPath.push({
        x: Math.round(gx * this.step + this.step / 2),
        y: Math.round(gy * this.step + this.step / 2),
      });
      trace = parent[trace];
    }
    rawPath.reverse();
    return simplifyPath(rawPath);
  }
}

function octileHeuristic(x1: number, y1: number, x2: number, y2: number): number {
  const dx = Math.abs(x1 - x2);
  const dy = Math.abs(y1 - y2);
  return 10 * (dx + dy) - 6 * Math.min(dx, dy);
}

function simplifyPath(points: Point2D[]): Point2D[] {
  if (points.length <= 2) return points;
  const out: Point2D[] = [points[0]];
  let prevDx = points[1].x - points[0].x;
  let prevDy = points[1].y - points[0].y;

  for (let i = 2; i < points.length; i++) {
    const dx = points[i].x - points[i - 1].x;
    const dy = points[i].y - points[i - 1].y;
    if (dx !== prevDx || dy !== prevDy) {
      out.push(points[i - 1]);
      prevDx = dx;
      prevDy = dy;
    }
  }
  out.push(points[points.length - 1]);
  return out;
}
