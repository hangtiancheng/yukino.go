import { useLocalStorageState } from "ahooks";
import { useCallback } from "react";

import { USER_ID_KEY } from "@/lib/api";

export function useUserId() {
  const [value, setValue] = useLocalStorageState<string>(USER_ID_KEY, {
    defaultValue: "1",
  });
  const userId = (value ?? "1").trim() || "1";
  const setUserId = useCallback((next: string) => setValue(next), [setValue]);
  return { userId, setUserId };
}
