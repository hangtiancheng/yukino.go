import { Send } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

interface ChatInputProps {
  streaming: boolean;
  onSend: (text: string) => void;
}

export function ChatInput({ streaming, onSend }: ChatInputProps) {
  const [text, setText] = useState("");
  const taRef = useRef<HTMLTextAreaElement>(null);

  // Auto-grow the textarea up to ~10 lines.
  useEffect(() => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = "0px";
    ta.style.height = `${Math.min(ta.scrollHeight, 240)}px`;
  }, [text]);

  const submit = () => {
    const value = text.trim();
    if (!value || streaming) return;
    setText("");
    onSend(value);
  };

  return (
    <div className="bg-background/80 shrink-0 border-t px-3 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] backdrop-blur lg:px-6">
      <div className="mx-auto flex w-full max-w-3xl items-end gap-2">
        <textarea
          ref={taRef}
          rows={1}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          placeholder="Type a message..."
          autoComplete="off"
          disabled={streaming}
          className="border-input placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 max-h-60 flex-1 resize-none rounded-xl border bg-transparent px-3.5 py-2.5 text-sm leading-relaxed transition-colors outline-none focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50"
        />
        <motion.div whileTap={{ scale: 0.92 }}>
          <Button
            onClick={submit}
            disabled={streaming || !text.trim()}
            size="icon-lg"
            className="rounded-xl"
            aria-label="Send message"
          >
            <AnimatePresence mode="wait" initial={false}>
              {streaming ? (
                <motion.span
                  key="spinner"
                  initial={{ opacity: 0, scale: 0.6 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.6 }}
                  transition={{ duration: 0.15 }}
                >
                  <Spinner />
                </motion.span>
              ) : (
                <motion.span
                  key="send"
                  initial={{ opacity: 0, scale: 0.6 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.6 }}
                  transition={{ duration: 0.15 }}
                >
                  <Send />
                </motion.span>
              )}
            </AnimatePresence>
          </Button>
        </motion.div>
      </div>
      <p className="text-muted-foreground mx-auto mt-2 hidden w-full max-w-3xl text-xs lg:block">
        Enter to send · Shift+Enter for a new line
      </p>
    </div>
  );
}
