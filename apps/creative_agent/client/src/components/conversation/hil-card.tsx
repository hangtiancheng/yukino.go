import { Play } from "lucide-react";
import { motion } from "motion/react";

import { Button } from "@/components/ui/button";

interface HilCardProps {
  content: string;
  disabled?: boolean;
  onResume: () => void;
}

/** HIL breakpoint card: shows the interrupt content plus a "Continue" button (resume restores the checkpoint). */
export function HilCard({ content, disabled, onResume }: HilCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.96, y: 8 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      className="bg-card relative overflow-hidden rounded-2xl border border-amber-500/40 p-4 shadow-sm"
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-amber-400/70 to-transparent" />
      <div className="flex items-center gap-2">
        <span className="relative flex size-2.5">
          <motion.span
            className="absolute inline-flex size-full rounded-full bg-amber-400"
            animate={{ scale: [1, 1.9], opacity: [0.6, 0] }}
            transition={{ duration: 1.6, repeat: Infinity, ease: "easeOut" }}
          />
          <span className="relative inline-flex size-2.5 rounded-full bg-amber-400" />
        </span>
        <p className="text-sm font-medium">Confirmation needed</p>
      </div>
      <p className="text-muted-foreground mt-2 text-sm">{content}</p>
      <div className="mt-3">
        <Button size="sm" disabled={disabled} onClick={onResume}>
          <Play data-icon="inline-start" />
          Continue
        </Button>
      </div>
    </motion.div>
  );
}
