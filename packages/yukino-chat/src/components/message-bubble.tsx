/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { BASE_URL } from "@/config";
import { cn } from "@/lib/utils";
import { icon, icons } from "@/components/icons";
import "@/components/ui/avatar";
import { Badge } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import type { Message } from "@/types";

const FILE_MESSAGE = 2;
const STAGGER_STEP_MS = 40;
const STAGGER_CAP_MS = 320;

const downloadFile = (url: string, name: string) => {
  const fileUrl = url
    ? url.startsWith("http")
      ? url
      : BASE_URL + url
    : BASE_URL + "/static/files/" + name;
  const saveName = name || "download";
  fetch(fileUrl)
    .then((r) => {
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.blob();
    })
    .then((blob) => {
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = saveName;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(link.href);
    })
    .catch(() => {});
};

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function initialOf(name: string): string {
  return name.trim().charAt(0).toUpperCase() || "?";
}

function FileAttachment({
  message,
  isSelf,
}: {
  message: Message;
  isSelf: boolean;
}) {
  const fileName = message.file_name || "file";
  return (
    <div className="flex items-center gap-3">
      <span
        className={cn(
          "flex size-10 shrink-0 items-center justify-center rounded-xl transition-colors duration-300",
          isSelf
            ? "bg-primary/40 text-primary-foreground"
            : "bg-primary/15 text-primary-deep",
        )}
      >
        {icon(icons.FileText, "size-5")}
      </span>
      <div className="flex min-w-0 flex-col items-start gap-1.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium">{fileName}</span>
          {message.file_size && (
            <Badge
              variant={isSelf ? "default" : "secondary"}
              className="px-1.5 py-0 text-[10px]"
            >
              {message.file_size}
            </Badge>
          )}
        </div>
        <Button
          variant={isSelf ? "default" : "outline"}
          size="xs"
          onClick={() => downloadFile(message.url, fileName)}
        >
          {icon(icons.Download, "size-3")}
          Download
        </Button>
      </div>
    </div>
  );
}

export interface MessageListProps {
  messages: Message[];
  currentUserId: string;
  currentUserAvatar: string;
  currentUserName: string;
}

export function MessageList({
  messages,
  currentUserId,
  currentUserAvatar,
  currentUserName,
}: MessageListProps) {
  if (messages.length === 0) {
    return (
      <div className="animate-in fade-in flex flex-1 flex-col items-center justify-center gap-3 py-20 duration-500">
        <span className="bg-primary/15 flex size-12 items-center justify-center rounded-full">
          {icon(icons.MessageCircle, "size-5")}
        </span>
        <p className="text-muted-foreground/60 text-sm">No messages yet</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {messages.map((message, index) => {
        const isSelf = message.send_id === currentUserId;
        const name = isSelf ? currentUserName : message.send_name;
        const avatar = isSelf ? currentUserAvatar : message.send_avatar;
        const isFile = message.type === FILE_MESSAGE;

        return (
          <div
            key={
              message.uuid ||
              `${message.send_id}-${message.created_at}-${message.type}-${index}`
            }
            className={cn(
              "animate-in fade-in slide-in-from-bottom-2 flex items-start gap-2.5 duration-300",
              isSelf && "flex-row-reverse",
            )}
            style={{
              animationDelay: `${Math.min(index * STAGGER_STEP_MS, STAGGER_CAP_MS)}ms`,
            }}
          >
            <x-avatar
              className={cn(
                "size-9 shrink-0",
                isSelf && "ring-primary/40 ring-2",
              )}
              src={avatar}
              name={name || initialOf(name)}
            />

            <div
              className={cn(
                "flex min-w-0 flex-col gap-1",
                isSelf ? "items-end" : "items-start",
              )}
            >
              <span className="text-muted-foreground flex items-baseline gap-2 px-1 text-xs">
                <span className="font-medium">{name}</span>
                <span className="tabular-nums opacity-70">
                  {formatTime(message.created_at)}
                </span>
              </span>

              <div
                className={cn(
                  "max-w-[70%] rounded-2xl px-3.5 py-2.5 text-sm leading-relaxed break-words transition-shadow duration-300",
                  isSelf
                    ? "bg-primary text-primary-foreground rounded-br-md shadow-sm hover:shadow-md"
                    : "border-border bg-card text-foreground rounded-bl-md border shadow-sm hover:shadow-md",
                )}
              >
                {isFile ? (
                  <FileAttachment message={message} isSelf={isSelf} />
                ) : (
                  <p className="whitespace-pre-wrap">{message.content}</p>
                )}
              </div>

              {isSelf && isFile && (
                <span className="text-muted-foreground flex items-center gap-1 px-1 text-xs opacity-70">
                  {icon(icons.CheckCheck, "size-3")}
                  Sent
                </span>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
