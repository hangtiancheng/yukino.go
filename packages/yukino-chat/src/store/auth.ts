import { signal } from "@lit-labs/signals";
import type { UserInfo } from "../types";
import { resolveAvatar } from "../utils/avatar";

const TOKEN_KEY = "token";

export interface AuthSnapshot {
  user: UserInfo;
  token: string;
  loggedIn: boolean;
}

export const emptyUser: UserInfo = {
  uuid: "",
  nickname: "",
  telephone: "",
  email: "",
  avatar: "",
  gender: 0,
  birthday: "",
  signature: "",
  status: 0,
  is_admin: 0,
  created_at: "",
};

export function getToken(): string {
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

function loadUserInfo(): UserInfo {
  try {
    const raw = sessionStorage.getItem("userInfo");
    return raw ? JSON.parse(raw) : { ...emptyUser };
  } catch {
    return { ...emptyUser };
  }
}

const initialUser = loadUserInfo();

export const authStore = signal<AuthSnapshot>({
  user: initialUser,
  token: getToken(),
  loggedIn: !!initialUser.uuid,
});

export function currentUser(): UserInfo {
  return authStore.get().user;
}

export function isLoggedIn(): boolean {
  return authStore.get().loggedIn;
}

export function setLogin(token: string, user: UserInfo): void {
  user.avatar = resolveAvatar(user.avatar, user.uuid);
  sessionStorage.setItem("userInfo", JSON.stringify(user));
  sessionStorage.setItem(TOKEN_KEY, token);
  authStore.set({ user, token, loggedIn: !!user.uuid });
}

export function updateUserInfo(user: UserInfo): void {
  user.avatar = resolveAvatar(user.avatar, user.uuid);
  sessionStorage.setItem("userInfo", JSON.stringify(user));
  authStore.set({ ...authStore.get(), user, loggedIn: !!user.uuid });
}

export function clearLogin(): void {
  sessionStorage.removeItem("userInfo");
  sessionStorage.removeItem(TOKEN_KEY);
  authStore.set({ user: { ...emptyUser }, token: "", loggedIn: false });
}
