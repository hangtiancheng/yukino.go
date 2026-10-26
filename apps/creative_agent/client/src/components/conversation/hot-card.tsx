import { ImageIcon } from "lucide-react";

import { cn } from "@/lib/utils";
import type { HotCardUiData, PromptItem } from "@/stores/conversation";

interface HotCardProps {
  uiData: HotCardUiData;
  msgId?: string;
  disabled?: boolean;
  onSelect: (item: PromptItem, msgId?: string) => void;
}

/** 热点卡/创作功能卡：渲染 uiData.promptItems 为可点击选项列表。 */
export function HotCard({ uiData, msgId, disabled, onSelect }: HotCardProps) {
  const label = uiData.label || "为您找到了几个热点选题，请挑选一个试试呢～";
  const isImageText = uiData.layout === "image-text";
  const items = uiData.promptItems ?? [];

  return (
    <div className="bg-card text-card-foreground flex flex-col gap-3 rounded-xl border p-4 shadow-sm">
      <p className="text-muted-foreground text-sm">{label}</p>
      <div className="flex flex-col gap-2">
        {items.map((item, i) => {
          const clickable = Boolean(item.eid || item.pipeline);
          return (
            <button
              key={`${item.eid ?? item.title}-${i}`}
              type="button"
              disabled={disabled || !clickable}
              onClick={() => onSelect(item, msgId)}
              className={cn(
                "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                clickable && !disabled
                  ? "hover:border-primary/50 hover:bg-accent cursor-pointer"
                  : "cursor-default opacity-60",
              )}
            >
              {isImageText ? (
                item.imageUrl ? (
                  <img
                    src={item.imageUrl}
                    alt=""
                    className="size-10 shrink-0 rounded-md object-cover"
                    onError={(e) => {
                      e.currentTarget.style.display = "none";
                    }}
                  />
                ) : (
                  <span className="bg-muted flex size-10 shrink-0 items-center justify-center rounded-md">
                    <ImageIcon className="text-muted-foreground size-5" />
                  </span>
                )
              ) : null}
              <span className="flex min-w-0 flex-col gap-1">
                <span className="text-sm font-medium">{item.title}</span>
                {item.text ? (
                  <span className="text-muted-foreground text-xs">
                    {item.text}
                  </span>
                ) : null}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
