import { CheckCircle2 } from "lucide-react";
import { motion } from "motion/react";

import { Button } from "@/components/ui/button";

interface ScriptCardProps {
  disabled?: boolean;
  onConfirm: () => void;
}

/** Script confirmation card: the script is generated; after the user confirms, video synthesis is dispatched. */
export function ScriptCard({ disabled, onConfirm }: ScriptCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.96, y: 8 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      className="border-primary/30 bg-card relative overflow-hidden rounded-2xl border p-4 shadow-sm"
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-violet-400/70 to-transparent" />
      <div className="flex items-center gap-2">
        <span className="flex size-6 items-center justify-center rounded-lg bg-violet-500/10">
          <CheckCircle2 className="size-4 text-violet-500" />
        </span>
        <p className="text-sm font-medium">Script generated, please confirm</p>
      </div>
      <p className="text-muted-foreground mt-2 text-sm">
        The script content is above; confirm to start video synthesis.
      </p>
      <div className="mt-3">
        <Button size="sm" disabled={disabled} onClick={onConfirm}>
          <CheckCircle2 data-icon="inline-start" />
          Confirm script
        </Button>
      </div>
    </motion.div>
  );
}
