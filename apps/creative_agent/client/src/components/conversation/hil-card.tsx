import { Play } from "lucide-react";

import { Button } from "@/components/ui/button";

interface HilCardProps {
  content: string;
  disabled?: boolean;
  onResume: () => void;
}

/** HIL breakpoint card: shows the interrupt content plus a "Continue" button (resume restores the checkpoint). */
export function HilCard({ content, disabled, onResume }: HilCardProps) {
  return (
    <div className="bg-card flex flex-col gap-2 rounded-xl border border-amber-500/40 p-4 shadow-sm">
      <p className="text-sm font-medium">Confirmation needed</p>
      <p className="text-muted-foreground text-sm">{content}</p>
      <div>
        <Button size="sm" disabled={disabled} onClick={onResume}>
          <Play data-icon="inline-start" />
          Continue
        </Button>
      </div>
    </div>
  );
}
