import { Play } from "lucide-react";

import { Button } from "@/components/ui/button";

interface HilCardProps {
  content: string;
  disabled?: boolean;
  onResume: () => void;
}

/** HIL 断点卡：展示中断内容 + "继续"按钮（resume 恢复 checkpoint）。 */
export function HilCard({ content, disabled, onResume }: HilCardProps) {
  return (
    <div className="bg-card flex flex-col gap-2 rounded-xl border border-amber-500/40 p-4 shadow-sm">
      <p className="text-sm font-medium">需要你确认</p>
      <p className="text-muted-foreground text-sm">{content}</p>
      <div>
        <Button size="sm" disabled={disabled} onClick={onResume}>
          <Play data-icon="inline-start" />
          继续
        </Button>
      </div>
    </div>
  );
}
