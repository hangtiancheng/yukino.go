export interface JwtClaims {
  username?: string;
  user_id?: number;
  jti?: string;
  exp?: number;
}

/** 解析 JWT payload（不校验签名，仅用于展示用户名等）。 */
export function parseJwt(token: string): JwtClaims | null {
  try {
    const parts = token.split(".");
    if (parts.length < 2) return null;
    const base64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(atob(base64)) as JwtClaims;
  } catch {
    return null;
  }
}
