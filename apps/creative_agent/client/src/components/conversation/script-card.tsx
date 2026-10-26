import { CheckCircle2 } from "lucide-react";

import { Button } from "@/components/ui/button";

interface ScriptCardProps {
  disabled?: boolean;
  onConfirm: () => void;
}

/** 脚本确认卡：脚本已生成，用户确认后投递视频合成。 */
export function ScriptCard({ disabled, onConfirm }: ScriptCardProps) {
  return (
    <div className="border-primary/30 bg-card flex flex-col gap-2 rounded-xl border p-4 shadow-sm">
      <p className="text-sm font-medium">脚本已生成，请确认</p>
      <p className="text-muted-foreground text-sm">
        脚本内容见上方，确认后开始视频合成。
      </p>
      <div>
        <Button size="sm" disabled={disabled} onClick={onConfirm}>
          <CheckCircle2 data-icon="inline-start" />
          确认脚本
        </Button>
      </div>
    </div>
  );
}
