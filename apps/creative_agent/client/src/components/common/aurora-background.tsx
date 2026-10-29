import { motion } from "motion/react";

import { cn } from "@/lib/utils";

interface AuroraBackgroundProps {
  className?: string;
  /** "dim" for light surfaces behind content, default for dark brand panels. */
  intensity?: "default" | "dim";
}

/**
 * Slowly drifting gradient blobs + optional grid overlay. Pure CSS gradients
 * animated with motion — no image assets are used anywhere in the app.
 */
export function AuroraBackground({
  className,
  intensity = "default",
}: AuroraBackgroundProps) {
  const dim = intensity === "dim";
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-0 overflow-hidden",
        className,
      )}
    >
      <motion.div
        className={cn(
          "absolute -top-40 -left-32 size-[34rem] rounded-full blur-3xl",
          dim ? "bg-violet-500/15" : "bg-violet-500/30",
        )}
        animate={{
          x: [0, 70, -30, 0],
          y: [0, 40, -20, 0],
          scale: [1, 1.12, 0.96, 1],
        }}
        transition={{ duration: 26, repeat: Infinity, ease: "easeInOut" }}
      />
      <motion.div
        className={cn(
          "absolute top-1/3 -right-40 size-[38rem] rounded-full blur-3xl",
          dim ? "bg-cyan-400/12" : "bg-cyan-400/25",
        )}
        animate={{
          x: [0, -60, 25, 0],
          y: [0, -45, 30, 0],
          scale: [1, 0.92, 1.08, 1],
        }}
        transition={{ duration: 32, repeat: Infinity, ease: "easeInOut" }}
      />
      <motion.div
        className={cn(
          "absolute -bottom-48 left-1/4 size-[30rem] rounded-full blur-3xl",
          dim ? "bg-fuchsia-500/12" : "bg-fuchsia-500/22",
        )}
        animate={{ x: [0, 45, -35, 0], y: [0, -30, 15, 0] }}
        transition={{ duration: 22, repeat: Infinity, ease: "easeInOut" }}
      />
    </div>
  );
}

/** Faint engineering-grid overlay for dark panels (CSS gradients only). */
export function GridOverlay({ className }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={cn(
        "absolute inset-0 bg-[linear-gradient(to_right,rgba(255,255,255,0.05)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.05)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_70%_60%_at_50%_40%,black_20%,transparent_100%)] bg-[size:44px_44px]",
        className,
      )}
    />
  );
}
