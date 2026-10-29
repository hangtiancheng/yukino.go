import { ArrowRight, TrendingUp } from "lucide-react";
import { motion } from "motion/react";

import { genCover } from "@/lib/avatar";
import { SPRING_SMOOTH } from "@/lib/motion";
import type { HotCardUiData, PromptItem } from "@/stores/conversation";

interface HotCardProps {
  uiData: HotCardUiData;
  msgId?: string;
  disabled?: boolean;
  onSelect: (item: PromptItem, msgId?: string) => void;
}

/**
 * Hot-topic / creation-feature card: renders uiData.promptItems as clickable
 * options. Thumbnails are generated on a canvas from the item seed — the app
 * never loads remote or bundled images.
 */
export function HotCard({ uiData, msgId, disabled, onSelect }: HotCardProps) {
  const label =
    uiData.label || "Here are some hot topics for you — pick one to try!";
  const isImageText = uiData.layout === "image-text";
  const items = uiData.promptItems ?? [];

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.96, y: 8 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      transition={SPRING_SMOOTH}
      className="bg-card text-card-foreground rounded-2xl border p-4 shadow-sm sm:p-5"
    >
      <div className="mb-3 flex items-center gap-2">
        <span className="flex size-6 items-center justify-center rounded-lg bg-violet-500/10">
          <TrendingUp className="size-3.5 text-violet-500" />
        </span>
        <p className="text-sm font-medium">{label}</p>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        {items.map((item, i) => {
          const clickable = Boolean(item.eid || item.pipeline);
          const seed = item.eid || item.title || `item-${i}`;
          return (
            <motion.button
              key={`${seed}-${i}`}
              type="button"
              disabled={disabled || !clickable}
              onClick={() => onSelect(item, msgId)}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ ...SPRING_SMOOTH, delay: 0.06 * i }}
              whileHover={clickable && !disabled ? { y: -2 } : undefined}
              whileTap={clickable && !disabled ? { scale: 0.98 } : undefined}
              className={
                clickable && !disabled
                  ? "group hover:bg-accent flex cursor-pointer items-start gap-3 rounded-xl border p-3 text-left transition-colors hover:border-violet-400/60"
                  : "flex items-start gap-3 rounded-xl border p-3 text-left opacity-60"
              }
            >
              {isImageText ? (
                <img
                  src={genCover(seed)}
                  alt=""
                  draggable={false}
                  className="size-11 shrink-0 rounded-lg object-cover"
                />
              ) : (
                <span className="bg-muted text-muted-foreground flex size-7 shrink-0 items-center justify-center rounded-lg text-xs font-semibold">
                  {i + 1}
                </span>
              )}
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="line-clamp-2 text-sm font-medium">
                  {item.title}
                </span>
                {item.text ? (
                  <span className="text-muted-foreground line-clamp-2 text-xs">
                    {item.text}
                  </span>
                ) : null}
              </span>
              {clickable ? (
                <ArrowRight className="text-muted-foreground/0 mt-1 size-4 shrink-0 transition-colors group-hover:text-violet-500" />
              ) : null}
            </motion.button>
          );
        })}
      </div>
    </motion.div>
  );
}
