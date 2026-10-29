/**
 * Deterministic, code-generated visuals (no image assets anywhere in the app).
 *
 * Every avatar / thumbnail is drawn on a <canvas> at runtime from a string
 * seed: FNV-1a hash -> xorShift32 PRNG -> symmetric pixel grid or gradient
 * composition -> data URL. The same seed always yields the same picture.
 */

function fnv1a(str: string): number {
  let hash = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    hash ^= str.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

function xorShift32(seed: number): () => number {
  let state = seed || 1;
  return () => {
    state ^= state << 13;
    state ^= state >>> 17;
    state ^= state << 5;
    state >>>= 0;
    return state / 0xffffffff;
  };
}

/** Tiny memoizer keyed on the first string argument. */
function memoize1<A extends string, R>(fn: (arg: A) => R): (arg: A) => R {
  const cache = new Map<string, R>();
  return (arg) => {
    const hit = cache.get(arg);
    if (hit !== undefined) return hit;
    const value = fn(arg);
    // Keep the cache bounded; avatars are cheap but sessions are long-lived.
    if (cache.size > 512) cache.clear();
    cache.set(arg, value);
    return value;
  };
}

function roundRectPath(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
) {
  const rr = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + rr, y);
  ctx.arcTo(x + w, y, x + w, y + h, rr);
  ctx.arcTo(x + w, y + h, x, y + h, rr);
  ctx.arcTo(x, y + h, x, y, rr);
  ctx.arcTo(x, y, x + w, y, rr);
  ctx.closePath();
}

/**
 * Identicon avatar: a mirrored 5x5 pixel grid in a single hue, clipped to a
 * rounded square. `seed` is usually the username or a message id.
 */
export const genIdenticon = memoize1((seed: string): string => {
  const rand = xorShift32(fnv1a(seed));

  const GRID = 5;
  const CELL = 40;
  const MARGIN = 28;
  const SIZE = GRID * CELL + MARGIN * 2;

  const canvas = document.createElement("canvas");
  canvas.width = SIZE;
  canvas.height = SIZE;
  const ctx = canvas.getContext("2d")!;

  const hue = Math.floor(rand() * 360);
  const saturation = 55 + Math.floor(rand() * 15);
  const lightness = 45 + Math.floor(rand() * 15);

  // Soft tinted background derived from the same hue.
  ctx.fillStyle = `hsl(${hue}, ${saturation}%, 94%)`;
  ctx.fillRect(0, 0, SIZE, SIZE);

  ctx.fillStyle = `hsl(${hue}, ${saturation}%, ${lightness}%)`;

  for (let col = 0; col < Math.ceil(GRID / 2); col++) {
    for (let row = 0; row < GRID; row++) {
      if (rand() >= 0.5) {
        roundRectPath(
          ctx,
          MARGIN + col * CELL + 2,
          MARGIN + row * CELL + 2,
          CELL - 4,
          CELL - 4,
          10,
        );
        ctx.fill();
        const mirrorCol = GRID - 1 - col;
        if (mirrorCol !== col) {
          roundRectPath(
            ctx,
            MARGIN + mirrorCol * CELL + 2,
            MARGIN + row * CELL + 2,
            CELL - 4,
            CELL - 4,
            10,
          );
          ctx.fill();
        }
      }
    }
  }
  return canvas.toDataURL();
});

/**
 * Generative "cover" thumbnail for list items (hot topics, features): layered
 * translucent radial blobs + a diagonal sheen on a two-tone gradient, all
 * derived from the seed. Used instead of remote/bundled images.
 */
export const genCover = memoize1((seed: string): string => {
  const rand = xorShift32(fnv1a(`cover:${seed}`));

  const W = 160;
  const H = 160;
  const canvas = document.createElement("canvas");
  canvas.width = W;
  canvas.height = H;
  const ctx = canvas.getContext("2d")!;

  const hue = Math.floor(rand() * 360);
  const hue2 = (hue + 40 + Math.floor(rand() * 80)) % 360;

  const base = ctx.createLinearGradient(0, 0, W, H);
  base.addColorStop(0, `hsl(${hue}, 70%, 62%)`);
  base.addColorStop(1, `hsl(${hue2}, 65%, 45%)`);
  ctx.fillStyle = base;
  ctx.fillRect(0, 0, W, H);

  // Translucent blobs.
  const blobs = 3 + Math.floor(rand() * 3);
  for (let i = 0; i < blobs; i++) {
    const cx = rand() * W;
    const cy = rand() * H;
    const r = W * (0.25 + rand() * 0.45);
    const bh = (hue + Math.floor(rand() * 120)) % 360;
    const g = ctx.createRadialGradient(cx, cy, 0, cx, cy, r);
    g.addColorStop(0, `hsla(${bh}, 80%, ${60 + rand() * 25}%, 0.55)`);
    g.addColorStop(1, `hsla(${bh}, 80%, 50%, 0)`);
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, W, H);
  }

  // Diagonal sheen for depth.
  const sheen = ctx.createLinearGradient(0, H, W, 0);
  sheen.addColorStop(0, "rgba(255,255,255,0)");
  sheen.addColorStop(0.5, "rgba(255,255,255,0.18)");
  sheen.addColorStop(1, "rgba(255,255,255,0)");
  ctx.fillStyle = sheen;
  ctx.fillRect(0, 0, W, H);

  return canvas.toDataURL();
});

/** Initial letter(s) used as a fallback visual next to generated avatars. */
export function initialsOf(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  const parts = trimmed.split(/[\s_-]+/).filter(Boolean);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}
