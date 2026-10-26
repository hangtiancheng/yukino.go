import {
  Bot,
  CircleHelp,
  Heart,
  LoaderCircle,
  Plus,
  Send,
  Sparkles,
  ThumbsDown,
  ThumbsUp,
  UserRound,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";

import { MealCard } from "@/components/shared/meal-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { useSaveFeedback } from "@/hooks/queries";
import { ApiError, TakeoutApi } from "@/lib/api";
import { SLOT_LABELS } from "@/lib/format";
import type { MealResponse, SourceMode } from "@/lib/schemas";
import { cn } from "@/lib/utils";
import {
  nextMessageId,
  useChatStore,
  type ChatMessage,
} from "@/stores/chat-store";

const QUICK_PROMPTS = [
  "Something quick and light for breakfast",
  "Low-fat dinner ideas for tonight",
  "Feeling down, want something warm",
  "Show me different options",
  "My stomach hurts, what should I eat?",
];

export default function ChatPage() {
  const {
    sessionId,
    sourceMode,
    sending,
    messages,
    setSessionId,
    setSourceMode,
    setSending,
    addMessage,
    clearMessages,
  } = useChatStore();
  const [draft, setDraft] = useState("");
  const [feedbackGiven, setFeedbackGiven] = useState<Record<string, string>>(
    {},
  );
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const feedback = useSaveFeedback();

  useEffect(() => {
    const pending = sessionStorage.getItem("takeout.pendingPrompt");
    if (pending) {
      sessionStorage.removeItem("takeout.pendingPrompt");
      setDraft(pending);
    }
  }, []);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages, sending]);

  const send = useCallback(
    async (rawText: string) => {
      const text = rawText.trim();
      if (!text || sending) return;
      setDraft("");
      addMessage({
        id: nextMessageId(),
        role: "user",
        text,
        createdAt: Date.now(),
      });
      setSending(true);
      try {
        const response = await TakeoutApi.chat({
          sessionId,
          message: text,
          sourceMode,
        });
        setSessionId(response.sessionId);
        addMessage({
          id: nextMessageId(),
          role: "assistant",
          text: response.speechText,
          createdAt: Date.now(),
          sessionId: response.sessionId,
          traceId: response.traceId,
          responseType: response.responseType,
          clarifyQuestion: response.clarifyQuestion,
          missingSlots: response.missingSlots,
          meals: response.displayBlocks,
        });
      } catch (error) {
        addMessage({
          id: nextMessageId(),
          role: "assistant",
          text:
            error instanceof Error
              ? error.message
              : "Request failed, please try again.",
          createdAt: Date.now(),
          isError: true,
        });
      } finally {
        setSending(false);
      }
    },
    [addMessage, sending, sessionId, sourceMode, setSending, setSessionId],
  );

  const submitFeedback = useCallback(
    (
      message: ChatMessage,
      meal: MealResponse,
      action: "LIKE" | "ADOPT" | "DISLIKE",
    ) => {
      if (!message.sessionId) {
        toast.error("This message has no session to attach feedback to.");
        return;
      }
      feedback.mutate(
        {
          sessionId: message.sessionId,
          itemId: meal.id,
          action,
          // Mirror the legacy console: a dislike rates 2/5, everything
          // else rates 5/5, so the evaluation pipeline can score feedback
          // from the rating column as well as the action.
          rating: action === "DISLIKE" ? 2 : 5,
        },
        {
          onSuccess: () => {
            setFeedbackGiven((prev) => ({
              ...prev,
              [`${message.id}:${meal.id}`]: action,
            }));
            toast.success(
              `Feedback recorded: ${action.toLowerCase()} for "${meal.name}".`,
            );
          },
          onError: (error) =>
            toast.error(
              error instanceof ApiError
                ? error.message
                : "Failed to save feedback.",
            ),
        },
      );
    },
    [feedback],
  );

  // The backend binds the source mode to a session (the stored mode wins
  // over the request), so switching libraries mid-conversation would
  // silently keep searching the old one. Start a fresh session instead.
  const changeSourceMode = (mode: SourceMode) => {
    if (mode === sourceMode) return;
    setSourceMode(mode);
    if (sessionId) {
      clearMessages();
      toast.info(
        "Each session sticks to one library, so I started a fresh conversation.",
      );
    }
  };

  const empty = messages.length === 0;

  return (
    <div className="mx-auto flex h-[calc(100vh-6.5rem)] max-w-4xl flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="bg-card inline-flex rounded-lg border p-0.5">
          {(["PERSONAL", "PUBLIC"] as SourceMode[]).map((mode) => (
            <button
              key={mode}
              type="button"
              onClick={() => changeSourceMode(mode)}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                sourceMode === mode
                  ? "bg-primary text-primary-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {mode === "PERSONAL" ? "Personal library" : "Public library"}
            </button>
          ))}
        </div>
        <Badge
          variant="outline"
          className="text-muted-foreground hidden font-normal sm:inline-flex"
        >
          Session: {sessionId ? `${sessionId.slice(0, 8)}…` : "not started"}
        </Badge>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto"
          onClick={() => {
            clearMessages();
            toast.info("Started a new session.");
          }}
        >
          <Plus />
          New session
        </Button>
      </div>

      <Card className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl">
        <ScrollArea className="flex-1">
          <div className="space-y-5 p-4 md:p-6">
            {empty && (
              <div className="flex flex-col items-center gap-3 py-12 text-center">
                <span className="bg-primary/10 text-primary flex size-12 items-center justify-center rounded-2xl">
                  <Bot className="size-6" />
                </span>
                <div>
                  <p className="font-medium">Hi, I am your takeout agent.</p>
                  <p className="text-muted-foreground mt-1 max-w-md text-sm">
                    Describe your mood, scene, and health goals, and I will
                    recommend meals from the{" "}
                    {sourceMode === "PERSONAL" ? "personal" : "public"} library.
                  </p>
                </div>
                <div className="flex max-w-xl flex-wrap justify-center gap-1.5">
                  {QUICK_PROMPTS.map((prompt) => (
                    <button
                      key={prompt}
                      type="button"
                      onClick={() => void send(prompt)}
                      className="bg-background text-muted-foreground hover:border-primary/40 hover:text-foreground rounded-full border px-3 py-1.5 text-xs transition-colors"
                    >
                      {prompt}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {messages.map((message) =>
              message.role === "user" ? (
                <div key={message.id} className="flex justify-end">
                  <div className="flex max-w-[85%] flex-col items-end gap-1 md:max-w-[70%]">
                    <div className="flex items-start gap-2">
                      <div className="bg-primary text-primary-foreground rounded-2xl rounded-tr-sm px-4 py-2.5 text-sm shadow-sm">
                        {message.text}
                      </div>
                      <span className="bg-secondary text-secondary-foreground mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full">
                        <UserRound className="size-3.5" />
                      </span>
                    </div>
                    <span className="text-muted-foreground pr-9 text-xs tabular-nums opacity-70">
                      {new Date(message.createdAt).toLocaleTimeString(
                        undefined,
                        {
                          hour: "2-digit",
                          minute: "2-digit",
                        },
                      )}
                    </span>
                  </div>
                </div>
              ) : (
                <div key={message.id} className="flex justify-start">
                  <div className="flex max-w-[92%] items-start gap-2 md:max-w-[85%]">
                    <span
                      className={cn(
                        "mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full",
                        message.isError
                          ? "bg-destructive/10 text-destructive"
                          : "bg-primary/10 text-primary",
                      )}
                    >
                      <Bot className="size-3.5" />
                    </span>
                    <div className="min-w-0 flex-1 space-y-3">
                      <div
                        className={cn(
                          "bg-card rounded-2xl rounded-tl-sm border px-4 py-2.5 text-sm shadow-sm",
                          message.isError &&
                            "border-destructive/40 text-destructive",
                        )}
                      >
                        <p className="whitespace-pre-wrap">{message.text}</p>
                      </div>

                      {message.responseType === "CLARIFY" &&
                        message.missingSlots &&
                        message.missingSlots.length > 0 && (
                          <div className="bg-muted/50 flex flex-wrap items-center gap-1.5 rounded-lg border px-3 py-2">
                            <CircleHelp className="text-muted-foreground size-3.5" />
                            <span className="text-muted-foreground text-xs">
                              Missing slots:
                            </span>
                            {message.missingSlots.map((slot) => (
                              <Badge
                                key={slot}
                                variant="secondary"
                                className="font-normal"
                              >
                                {SLOT_LABELS[slot] ?? slot}
                              </Badge>
                            ))}
                          </div>
                        )}

                      {message.meals && message.meals.length > 0 && (
                        <div className="grid gap-3 sm:grid-cols-2">
                          {message.meals.map((meal) => (
                            <MealCard
                              key={meal.id}
                              meal={meal}
                              footer={
                                <div className="flex items-center gap-1">
                                  {(
                                    [
                                      {
                                        action: "LIKE",
                                        icon: ThumbsUp,
                                        label: "Like",
                                        activeClass:
                                          "text-primary bg-primary/10",
                                      },
                                      {
                                        action: "ADOPT",
                                        icon: Heart,
                                        label: "Adopt",
                                        activeClass:
                                          "text-rose-500 bg-rose-500/10",
                                      },
                                      {
                                        action: "DISLIKE",
                                        icon: ThumbsDown,
                                        label: "Dislike",
                                        activeClass:
                                          "text-destructive bg-destructive/10",
                                      },
                                    ] as const
                                  ).map(
                                    ({
                                      action,
                                      icon: Icon,
                                      label,
                                      activeClass,
                                    }) => {
                                      const given =
                                        feedbackGiven[
                                          `${message.id}:${meal.id}`
                                        ] === action;
                                      return (
                                        <Button
                                          key={action}
                                          variant="ghost"
                                          size="icon"
                                          className={cn(
                                            "size-7 transition-colors",
                                            given
                                              ? activeClass
                                              : "text-muted-foreground hover:text-primary",
                                          )}
                                          aria-label={`${label} ${meal.name}`}
                                          aria-pressed={given}
                                          onClick={() =>
                                            submitFeedback(
                                              message,
                                              meal,
                                              action,
                                            )
                                          }
                                        >
                                          <Icon className="size-3.5" />
                                        </Button>
                                      );
                                    },
                                  )}
                                </div>
                              }
                            />
                          ))}
                        </div>
                      )}

                      <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
                        <span className="tabular-nums opacity-70">
                          {new Date(message.createdAt).toLocaleTimeString(
                            undefined,
                            {
                              hour: "2-digit",
                              minute: "2-digit",
                            },
                          )}
                        </span>
                        {message.responseType && !message.isError && (
                          <Badge variant="outline" className="font-normal">
                            {message.responseType}
                          </Badge>
                        )}
                        {message.traceId && (
                          <Link
                            to={`/traces?traceId=${encodeURIComponent(message.traceId)}`}
                            className="hover:text-foreground inline-flex items-center gap-1 underline-offset-2 hover:underline"
                          >
                            <Sparkles className="size-3" />
                            trace {message.traceId.slice(0, 8)}…
                          </Link>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              ),
            )}

            {sending && (
              <div className="flex justify-start">
                <div className="flex items-start gap-2">
                  <span className="bg-primary/10 text-primary mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full">
                    <Bot className="size-3.5" />
                  </span>
                  <div className="bg-card rounded-2xl rounded-tl-sm border px-4 py-3 shadow-sm">
                    <div
                      className="flex items-center gap-1.5"
                      role="status"
                      aria-label="The agent is thinking"
                    >
                      <span className="bg-primary/70 size-1.5 animate-bounce rounded-full [animation-delay:-0.3s]" />
                      <span className="bg-primary/70 size-1.5 animate-bounce rounded-full [animation-delay:-0.15s]" />
                      <span className="bg-primary/70 size-1.5 animate-bounce rounded-full" />
                      <span className="text-muted-foreground ml-1.5 text-xs">
                        Thinking about what you should eat…
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            )}
            <div ref={bottomRef} />
          </div>
        </ScrollArea>

        <Separator />
        <form
          className="flex items-end gap-2 p-3"
          onSubmit={(event) => {
            event.preventDefault();
            void send(draft);
          }}
        >
          <Textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void send(draft);
              }
            }}
            placeholder="e.g. I want a light dinner that is easy to make…"
            className="min-h-11 flex-1 resize-none"
            rows={1}
          />
          <Button
            type="submit"
            size="icon"
            className="size-11"
            disabled={sending || !draft.trim()}
          >
            {sending ? <LoaderCircle className="animate-spin" /> : <Send />}
          </Button>
        </form>
      </Card>
    </div>
  );
}
