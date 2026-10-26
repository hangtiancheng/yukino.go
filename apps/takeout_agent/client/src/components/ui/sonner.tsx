import { useStore } from "@tanstack/react-store";
import { Toaster as Sonner, type ToasterProps } from "sonner";

import { preferencesStore } from "@/stores/preferences";

const Toaster = ({ ...props }: ToasterProps) => {
  const theme = useStore(preferencesStore).theme;

  return (
    <Sonner
      theme={theme}
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as React.CSSProperties
      }
      {...props}
    />
  );
};

export { Toaster };
