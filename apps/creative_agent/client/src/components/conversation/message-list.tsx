import { Bot, MessageSquare } from "lucide-react";
import { useEffect, useRef } from "react";

import { cn } from "@/lib/utils";
import type { ChatMessage } from "@/stores/conversation";

import { HilCard } from "./hil-card";
import { HotCard } from "./hot-card";
import { ScriptCard } from "./script-card";

interface MessageListProps {
  messages: ChatMessage[];
  streaming: boolean;
  onResume: (msgId: string) => void;
  onHotSelect: (
    item: import("@/stores/conversation").PromptItem,
    msgId?: string,
  ) => void;
  onScriptConfirm: () => void;
}

export function MessageList({
  messages,
  streaming,
  onResume,
  onHotSelect,
  onScriptConfirm,
}: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-3">
        <MessageSquare className="size-10" />
        <p>输入你的问题，开始对话</p>
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
      {messages.map((msg) => {
        switch (msg.kind) {
          case "user":
            return (
              <div key={msg.id} className="flex justify-end">
                <div className="bg-primary text-primary-foreground max-w-[80%] rounded-2xl rounded-br-md px-4 py-2 text-sm whitespace-pre-wrap">
                  {msg.content}
                </div>
              </div>
            );
          case "assistant":
            return (
              <div key={msg.id} className="flex items-start gap-2">
                <span className="bg-muted mt-1 flex size-7 shrink-0 items-center justify-center rounded-full">
                  <Bot className="text-muted-foreground size-4" />
                </span>
                <div className="bg-card text-card-foreground max-w-[80%] rounded-2xl rounded-tl-md border px-4 py-2 text-sm whitespace-pre-wrap">
                  {msg.content}
                </div>
              </div>
            );
          case "tool":
            return (
              <div key={msg.id} className="flex justify-center">
                <p className="bg-muted text-muted-foreground max-w-[90%] rounded-lg px-3 py-1.5 text-xs">
                  {msg.content}
                </p>
              </div>
            );
          case "error":
            return (
              <div key={msg.id} className="flex justify-center">
                <p className="bg-destructive/10 text-destructive max-w-[90%] rounded-lg px-3 py-1.5 text-xs">
                  {msg.content}
                </p>
              </div>
            );
          case "hil-card":
            return (
              <div key={msg.id} className={cn("flex justify-center")}>
                <div className="w-full max-w-md">
                  <HilCard
                    content={msg.content}
                    disabled={streaming}
                    onResume={() => onResume(msg.msgId ?? "")}
                  />
                </div>
              </div>
            );
          case "hot-card":
            return (
              <div key={msg.id} className="flex justify-center">
                <div className="w-full max-w-lg">
                  <HotCard
                    uiData={msg.uiData}
                    msgId={msg.msgId}
                    disabled={streaming}
                    onSelect={onHotSelect}
                  />
                </div>
              </div>
            );
          case "script-card":
            return (
              <div key={msg.id} className="flex justify-center">
                <div className="w-full max-w-md">
                  <ScriptCard
                    disabled={streaming}
                    onConfirm={onScriptConfirm}
                  />
                </div>
              </div>
            );
        }
      })}
      <div ref={bottomRef} />
    </div>
  );
}
