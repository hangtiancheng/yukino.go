import { create } from "zustand";
import { persist } from "zustand/middleware";

import { api } from "@/lib/api";
import { parseJwt, type JwtClaims } from "@/lib/jwt";

interface AuthState {
  token: string | null;
  setToken: (token: string) => void;
  clearToken: () => void;
  login: (username: string, password: string) => Promise<void>;
  register: (
    username: string,
    password: string,
    email: string,
  ) => Promise<void>;
  logout: () => Promise<void>;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: null,

      setToken: (token) => set({ token }),

      clearToken: () => set({ token: null }),

      login: async (username, password) => {
        const data = await api.post<{ token: string }>("/api/v1/login", {
          username,
          password,
        });
        set({ token: data.token });
      },

      register: async (username, password, email) => {
        await api.post("/api/v1/register", { username, password, email });
      },

      logout: async () => {
        const { token } = get();
        if (token) {
          try {
            await api.post("/api/v1/logout");
          } catch {
            // 网络失败也要清理本地状态
          }
        }
        set({ token: null });
      },
    }),
    { name: "auth" },
  ),
);

/** 当前 token 的 claims（用户名 / user_id）。 */
export function currentClaims(): JwtClaims | null {
  const token = useAuthStore.getState().token;
  return token ? parseJwt(token) : null;
}
