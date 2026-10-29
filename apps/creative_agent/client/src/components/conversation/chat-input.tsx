import { Send } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

interface ChatInputProps {
  streaming: boolean;
  onSend: (text: string) => void;
}

export function ChatInput({ streaming, onSend }: ChatInputProps) {
  const [text, setText] = useState("");

  const submit = () => {
    const value = text.trim();
    if (!value || streaming) return;
    setText("");
    onSend(value);
  };

  return (
    <div className="bg-background flex items-center gap-2 border-t p-4">
      <Input
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
        className="flex-1"
      />
      <Button onClick={submit} disabled={streaming || !text.trim()}>
        {streaming ? (
          <>
            <Spinner data-icon="inline-start" />
            Generating...
          </>
        ) : (
          <>
            <Send data-icon="inline-start" />
            Send
          </>
        )}
      </Button>
    </div>
  );
}
