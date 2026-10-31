import { useCallback } from "react";

import { useLocalStorageState } from "@/hooks/use-local-storage-state";
import { USER_ID_KEY } from "@/lib/api";

export function useUserId() {
  const [value, setValue] = useLocalStorageState(USER_ID_KEY, "1");
  const userId = (value ?? "1").trim() || "1";
  const setUserId = useCallback((next: string) => setValue(next), [setValue]);
  return { userId, setUserId };
}
