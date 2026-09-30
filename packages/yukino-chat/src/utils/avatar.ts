import { BASE_URL } from "../config";

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

export function genIdenticon(seed: string): string {
  const rand = xorShift32(fnv1a(seed));

  const GRID = 5;
  const CELL = 40;
  const MARGIN = 28;
  const SIZE = GRID * CELL + MARGIN * 2;

  const canvas = document.createElement("canvas");
  canvas.width = SIZE;
  canvas.height = SIZE;
  const ctx = canvas.getContext("2d")!;

  ctx.fillStyle = "#f0f0f0";
  ctx.fillRect(0, 0, SIZE, SIZE);

  const hue = 20 + Math.floor(rand() * 45);
  const saturation = 45 + Math.floor(rand() * 18);
  const lightness = 62 + Math.floor(rand() * 16);
  ctx.fillStyle = `hsl(${hue}, ${saturation}%, ${lightness}%)`;

  for (let col = 0; col < Math.ceil(GRID / 2); col++) {
    for (let row = 0; row < GRID; row++) {
      if (rand() >= 0.5) {
        ctx.fillRect(MARGIN + col * CELL, MARGIN + row * CELL, CELL, CELL);
        const mirrorCol = GRID - 1 - col;
        if (mirrorCol !== col) {
          ctx.fillRect(
            MARGIN + mirrorCol * CELL,
            MARGIN + row * CELL,
            CELL,
            CELL,
          );
        }
      }
    }
  }
  return canvas.toDataURL();
}

const LEGACY_DEFAULT = "https://vitejs.dev/logo.svg";

export function resolveAvatar(avatar: string, seed?: string): string {
  if (!avatar || avatar === LEGACY_DEFAULT) {
    return seed ? genIdenticon(seed) : "";
  }
  if (avatar.startsWith("http")) return avatar;
  return BASE_URL + avatar;
}
