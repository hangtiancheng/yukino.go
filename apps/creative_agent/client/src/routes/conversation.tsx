import { Link, useNavigate } from "@tanstack/react-router";
import { Home, LogOut, Plus } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";

import { ChatInput } from "@/components/conversation/chat-input";
import { MessageList } from "@/components/conversation/message-list";
import { Avatar } from "@/components/common/avatar";
import { BrandMark } from "@/components/common/brand-mark";
import { Button } from "@/components/ui/button";
import { EASE_OUT_EXPO } from "@/lib/motion";
import { currentClaims, useAuthStore } from "@/stores/auth";
import { useConversationStore } from "@/stores/conversation";

export function ConversationPage() {
  const navigate = useNavigate();
  const logout = useAuthStore((s) => s.logout);
  const username = currentClaims()?.username ?? "creator";

  const messages = useConversationStore((s) => s.messages);
  const streaming = useConversationStore((s) => s.streaming);
  const sendMessage = useConversationStore((s) => s.sendMessage);
  const resume = useConversationStore((s) => s.resume);
  const confirmHotOption = useConversationStore((s) => s.confirmHotOption);
  const confirmScriptCard = useConversationStore((s) => s.confirmScriptCard);
  const clearMessages = useConversationStore((s) => s.clearMessages);

  const onLogout = async () => {
    await logout();
    void navigate({ to: "/login" });
  };

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.35, ease: EASE_OUT_EXPO }}
      className="bg-background flex h-svh overflow-hidden"
    >
      {/* Desktop sidebar */}
      <aside className="bg-muted/40 hidden w-72 shrink-0 flex-col border-r lg:flex">
        <div className="flex items-center gap-2.5 px-5 py-5">
          <BrandMark className="size-9 rounded-xl" />
          <div className="flex flex-col">
            <span className="text-sm font-semibold tracking-tight">
              yukino-creative-agent
            </span>
            <span className="text-muted-foreground text-xs">
              Creative studio
            </span>
          </div>
        </div>

        <div className="flex flex-col gap-1.5 px-3">
          <Button
            className="justify-start"
            onClick={clearMessages}
            disabled={streaming}
          >
            <Plus data-icon="inline-start" />
            New chat
          </Button>
          <Button
            variant="ghost"
            className="justify-start"
            render={<Link to="/" />}
          >
            <Home data-icon="inline-start" />
            Home
          </Button>
        </div>

        <div className="mt-auto flex flex-col gap-3 p-3">
          <div className="border-border/60 bg-background/70 flex items-center gap-2.5 rounded-xl border p-2.5">
            <Avatar seed={username} className="size-9 rounded-full" />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-sm font-medium">{username}</span>
              <span className="text-muted-foreground truncate text-xs">
                Signed in
              </span>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onLogout}
              aria-label="Sign out"
              title="Sign out"
            >
              <LogOut />
            </Button>
          </div>
        </div>
      </aside>

      {/* Main column */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Top bar (compact on mobile, status on desktop) */}
        <header className="flex shrink-0 items-center justify-between gap-2 border-b px-3 py-2.5 lg:px-6 lg:py-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <BrandMark className="size-7 rounded-lg lg:hidden" />
            <h1 className="truncate text-sm font-semibold tracking-tight lg:text-base">
              Agent Chat
            </h1>
            <AnimatePresence>
              {streaming ? (
                <motion.span
                  initial={{ opacity: 0, scale: 0.85 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.85 }}
                  className="hidden items-center gap-1.5 rounded-full bg-violet-500/10 px-2.5 py-0.5 text-xs font-medium text-violet-600 sm:inline-flex"
                >
                  <motion.span
                    className="size-1.5 rounded-full bg-violet-500"
                    animate={{ opacity: [1, 0.3, 1] }}
                    transition={{ duration: 1.2, repeat: Infinity }}
                  />
                  Generating…
                </motion.span>
              ) : null}
            </AnimatePresence>
          </div>
          <div className="flex items-center gap-1.5 lg:hidden">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={clearMessages}
              disabled={streaming}
              aria-label="New chat"
              title="New chat"
            >
              <Plus />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              render={<Link to="/" />}
              aria-label="Back to home"
              title="Back to home"
            >
              <Home />
            </Button>
          </div>
        </header>

        <MessageList
          messages={messages}
          streaming={streaming}
          onResume={resume}
          onHotSelect={(item, msgId) => void confirmHotOption(item, msgId)}
          onScriptConfirm={() => void confirmScriptCard()}
          onSuggestion={sendMessage}
        />

        <ChatInput streaming={streaming} onSend={(text) => sendMessage(text)} />
      </div>
    </motion.div>
  );
}
