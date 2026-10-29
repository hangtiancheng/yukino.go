import { CheckCircle2 } from "lucide-react";

import { Button } from "@/components/ui/button";

interface ScriptCardProps {
  disabled?: boolean;
  onConfirm: () => void;
}

/** Script confirmation card: the script is generated; after the user confirms, video synthesis is dispatched. */
export function ScriptCard({ disabled, onConfirm }: ScriptCardProps) {
  return (
    <div className="border-primary/30 bg-card flex flex-col gap-2 rounded-xl border p-4 shadow-sm">
      <p className="text-sm font-medium">Script generated, please confirm</p>
      <p className="text-muted-foreground text-sm">
        The script content is above; confirm to start video synthesis.
      </p>
      <div>
        <Button size="sm" disabled={disabled} onClick={onConfirm}>
          <CheckCircle2 data-icon="inline-start" />
          Confirm script
        </Button>
      </div>
    </div>
  );
}
