import { api } from "../service/api";
import { disconnectWs } from "../store/ws";
import { clearLogin, currentUser } from "../store/auth";
import { clearSessions } from "../store/session";

/**
 * Tear down auth + websocket session. Callers should navigate
 * to "/login" after this resolves.
 */
export async function performLogout(): Promise<void> {
  const uid = currentUser().uuid;
  await api.wsLogout({ owner_id: uid });
  disconnectWs();
  clearSessions();
  clearLogin();
}
