import { useStore } from "@tanstack/react-store";
import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { preferencesStore, toggleTheme } from "@/stores/preferences";

export function ThemeToggle() {
  const theme = useStore(preferencesStore).theme;

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      aria-label="Toggle theme"
    >
      {theme === "dark" ? <Sun /> : <Moon />}
    </Button>
  );
}
