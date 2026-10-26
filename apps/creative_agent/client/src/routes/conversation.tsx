import { Link } from "@tanstack/react-router";
import { Home } from "lucide-react";

import { ChatInput } from "@/components/conversation/chat-input";
import { MessageList } from "@/components/conversation/message-list";
import { Button } from "@/components/ui/button";
import { useConversationStore } from "@/stores/conversation";

export function ConversationPage() {
  const messages = useConversationStore((s) => s.messages);
  const streaming = useConversationStore((s) => s.streaming);
  const sendMessage = useConversationStore((s) => s.sendMessage);
  const resume = useConversationStore((s) => s.resume);
  const confirmHotOption = useConversationStore((s) => s.confirmHotOption);
  const confirmScriptCard = useConversationStore((s) => s.confirmScriptCard);

  return (
    <div className="mx-auto flex h-svh max-w-3xl flex-col">
      <header className="flex items-center justify-between border-b p-4">
        <h1 className="text-lg font-semibold tracking-tight">智能对话</h1>
        <Button variant="outline" size="sm" render={<Link to="/" />}>
          <Home data-icon="inline-start" />
          返回首页
        </Button>
      </header>

      <MessageList
        messages={messages}
        streaming={streaming}
        onResume={resume}
        onHotSelect={(item, msgId) => void confirmHotOption(item, msgId)}
        onScriptConfirm={() => void confirmScriptCard()}
      />

      <ChatInput streaming={streaming} onSend={(text) => sendMessage(text)} />
    </div>
  );
}
