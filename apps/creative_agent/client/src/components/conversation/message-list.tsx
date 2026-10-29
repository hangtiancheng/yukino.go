import { ArrowDown } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";

import { BrandMark } from "@/components/common/brand-mark";
import { Button } from "@/components/ui/button";
import { SPRING_SMOOTH, fadeUp, staggerGroup } from "@/lib/motion";
import type { ChatMessage, PromptItem } from "@/stores/conversation";

import { HilCard } from "./hil-card";
import { HotCard } from "./hot-card";
import { ScriptCard } from "./script-card";

interface MessageListProps {
  messages: ChatMessage[];
  streaming: boolean;
  onResume: (msgId: string) => void;
  onHotSelect: (item: PromptItem, msgId?: string) => void;
  onScriptConfirm: () => void;
  onSuggestion: (text: string) => void;
}

/** Suggestion chips shown in the empty state (sent verbatim to the agent). */
const SUGGESTIONS = [
  "看看当前热点",
  "帮我找一些创作灵感",
  "生成一个短视频脚本",
];

function TypingDots() {
  return (
    <span className="flex items-center gap-1 py-1.5">
      {[0, 1, 2].map((i) => (
        <motion.span
          key={i}
          className="bg-muted-foreground/70 size-1.5 rounded-full"
          animate={{ y: [0, -4, 0], opacity: [0.4, 1, 0.4] }}
          transition={{
            duration: 0.9,
            repeat: Infinity,
            delay: i * 0.15,
            ease: "easeInOut",
          }}
        />
      ))}
    </span>
  );
}

export function MessageList({
  messages,
  streaming,
  onResume,
  onHotSelect,
  onScriptConfirm,
  onSuggestion,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);
  const [atBottom, setAtBottom] = useState(true);
  const mountedRef = useRef(false);

  const lastId = messages.length > 0 ? messages[messages.length - 1].id : null;

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    atBottomRef.current = near;
    setAtBottom(near);
  };

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (atBottomRef.current) {
      el.scrollTo({
        top: el.scrollHeight,
        // Jump instantly on first paint (restored history), glide afterwards.
        behavior: mountedRef.current ? "smooth" : "auto",
      });
    }
    mountedRef.current = true;
  }, [messages, streaming]);

  if (messages.length === 0) {
    return (
      <div className="relative flex flex-1 flex-col items-center justify-center overflow-y-auto p-6">
        <motion.div
          variants={staggerGroup(0.05, 0.08)}
          initial="hidden"
          animate="show"
          className="flex max-w-md flex-col items-center text-center"
        >
          <motion.div variants={fadeUp}>
            <motion.div
              animate={{ y: [0, -7, 0] }}
              transition={{
                duration: 3.4,
                repeat: Infinity,
                ease: "easeInOut",
              }}
            >
              <BrandMark className="size-14 rounded-2xl shadow-lg shadow-violet-500/25" />
            </motion.div>
          </motion.div>
          <motion.h2
            variants={fadeUp}
            className="mt-5 text-xl font-semibold tracking-tight"
          >
            Start creating
          </motion.h2>
          <motion.p
            variants={fadeUp}
            className="text-muted-foreground mt-2 text-sm leading-relaxed"
          >
            Ask for trending topics, draft a script, or kick off video synthesis
            — the agents will take it from there.
          </motion.p>
          <motion.div
            variants={fadeUp}
            className="mt-6 flex flex-wrap items-center justify-center gap-2"
          >
            {SUGGESTIONS.map((s) => (
              <motion.button
                key={s}
                type="button"
                whileHover={{ y: -2 }}
                whileTap={{ scale: 0.96 }}
                onClick={() => onSuggestion(s)}
                className="border-border/70 bg-background/80 hover:bg-accent cursor-pointer rounded-full border px-3.5 py-1.5 text-xs font-medium transition-colors hover:border-violet-400/60"
              >
                {s}
              </motion.button>
            ))}
          </motion.div>
        </motion.div>
      </div>
    );
  }

  return (
    <div className="relative min-h-0 flex-1">
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="h-full scrollbar-thin overflow-y-auto"
      >
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-3 py-5 sm:px-6">
          <AnimatePresence initial={false}>
            {messages.map((msg) => {
              const isStreamingThis =
                streaming && msg.id === lastId && msg.kind === "assistant";
              switch (msg.kind) {
                case "user":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, y: 12, x: 16 }}
                      animate={{ opacity: 1, y: 0, x: 0 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-end"
                    >
                      <div className="bg-primary text-primary-foreground max-w-[85%] rounded-2xl rounded-br-md px-4 py-2 text-sm whitespace-pre-wrap sm:max-w-[75%]">
                        {msg.content}
                      </div>
                    </motion.div>
                  );
                case "assistant":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, y: 12, x: -16 }}
                      animate={{ opacity: 1, y: 0, x: 0 }}
                      transition={SPRING_SMOOTH}
                      className="flex items-start gap-2.5"
                    >
                      <BrandMark className="mt-0.5 size-7 rounded-lg" />
                      <div className="bg-card text-card-foreground max-w-[85%] rounded-2xl rounded-tl-md border px-4 py-2 text-sm whitespace-pre-wrap sm:max-w-[75%]">
                        {msg.content === "…" && isStreamingThis ? (
                          <TypingDots />
                        ) : (
                          msg.content
                        )}
                        {isStreamingThis && msg.content !== "…" ? (
                          <motion.span
                            className="bg-primary/70 ml-0.5 inline-block h-4 w-[2px] translate-y-0.5 rounded-full align-text-bottom"
                            animate={{ opacity: [1, 0, 1] }}
                            transition={{ duration: 1, repeat: Infinity }}
                          />
                        ) : null}
                      </div>
                    </motion.div>
                  );
                case "tool":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, scale: 0.92 }}
                      animate={{ opacity: 1, scale: 1 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-center"
                    >
                      <p className="bg-muted/80 text-muted-foreground max-w-[90%] rounded-full px-3.5 py-1.5 text-xs">
                        {msg.content}
                      </p>
                    </motion.div>
                  );
                case "error":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, scale: 0.92 }}
                      animate={{ opacity: 1, scale: 1 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-center"
                    >
                      <p className="bg-destructive/10 text-destructive max-w-[90%] rounded-full px-3.5 py-1.5 text-xs">
                        {msg.content}
                      </p>
                    </motion.div>
                  );
                case "hil-card":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, y: 14 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-center"
                    >
                      <div className="w-full max-w-md">
                        <HilCard
                          content={msg.content}
                          disabled={streaming}
                          onResume={() => onResume(msg.msgId ?? "")}
                        />
                      </div>
                    </motion.div>
                  );
                case "hot-card":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, y: 14 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-center"
                    >
                      <div className="w-full max-w-2xl">
                        <HotCard
                          uiData={msg.uiData}
                          msgId={msg.msgId}
                          disabled={streaming}
                          onSelect={onHotSelect}
                        />
                      </div>
                    </motion.div>
                  );
                case "script-card":
                  return (
                    <motion.div
                      key={msg.id}
                      initial={{ opacity: 0, y: 14 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={SPRING_SMOOTH}
                      className="flex justify-center"
                    >
                      <div className="w-full max-w-md">
                        <ScriptCard
                          disabled={streaming}
                          onConfirm={onScriptConfirm}
                        />
                      </div>
                    </motion.div>
                  );
              }
            })}
          </AnimatePresence>
          <div ref={bottomRef} />
        </div>
      </div>

      {/* Floating scroll-to-bottom */}
      <AnimatePresence>
        {!atBottom ? (
          <motion.div
            initial={{ opacity: 0, y: 10, scale: 0.9 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.9 }}
            transition={SPRING_SMOOTH}
            className="absolute bottom-4 left-1/2 -translate-x-1/2"
          >
            <Button
              variant="outline"
              size="icon"
              className="rounded-full shadow-md"
              aria-label="Scroll to bottom"
              onClick={() => {
                atBottomRef.current = true;
                setAtBottom(true);
                scrollRef.current?.scrollTo({
                  top: scrollRef.current.scrollHeight,
                  behavior: "smooth",
                });
              }}
            >
              <ArrowDown />
            </Button>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
