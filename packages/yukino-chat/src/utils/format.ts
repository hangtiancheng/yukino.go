/**
 * Human-readable file size used when uploading files in chat.
 * Mirrors the source project's `getFileSize`.
 */
export function getFileSize(size: number): string {
  if (size < 1024) return size + "B";
  if (size < 1024 * 1024) return (size / 1024).toFixed(2) + "KB";
  if (size < 1024 * 1024 * 1024) return (size / 1024 / 1024).toFixed(2) + "MB";
  return (size / 1024 / 1024 / 1024).toFixed(2) + "GB";
}

/**
 * Compact size label for the dashboard cache rows.
 */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  return (bytes / 1024).toFixed(1) + " KB";
}

/**
 * Format a cache entry expiration (stored as nanoseconds) into a
 * relative countdown string for the dashboard.
 */
export function formatExpire(nanos: number): string {
  if (nanos <= 0 || nanos >= Number.MAX_SAFE_INTEGER) return "never";
  const ms = nanos / 1_000_000;
  const now = Date.now();
  const diff = ms - now;
  if (diff <= 0) return "expired";
  if (diff < 60_000) return Math.ceil(diff / 1000) + "s left";
  if (diff < 3_600_000) return Math.ceil(diff / 60_000) + "m left";
  return new Date(ms).toLocaleTimeString();
}
