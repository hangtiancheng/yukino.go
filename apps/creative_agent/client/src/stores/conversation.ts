import { create } from "zustand";
import { persist } from "zustand/middleware";

import { api, openSse, type SseEventName } from "@/lib/api";
import { currentClaims } from "@/stores/auth";

// ---------- Types ----------

export interface PromptItem {
  title: string;
  text?: string;
  index?: number;
  eid?: string;
  imageUrl?: string;
  pipeline?: string;
}

export interface HotCardUiData {
  label?: string;
  layout?: string;
  promptItems?: PromptItem[];
}

export type ChatMessage =
  | { id: string; kind: "user"; content: string }
  | { id: string; kind: "assistant"; content: string }
  | { id: string; kind: "tool"; content: string }
  | { id: string; kind: "error"; content: string }
  | { id: string; kind: "hil-card"; content: string; msgId?: string }
  | { id: string; kind: "hot-card"; uiData: HotCardUiData; msgId?: string }
  | { id: string; kind: "script-card"; msgId: string };

interface ClarificationResponse {
  stage?: string;
  pipeline?: string;
  agent?: string;
  nextAgent?: string;
  originMsgId?: string;
  selectedEid?: string;
  topicTitle?: string;
}

// ---------- sessionId (sessionStorage, isolated per tab) ----------

export function sessionId(): string {
  let sid = sessionStorage.getItem("convSessionId");
  if (!sid) {
    sid = crypto.randomUUID();
    sessionStorage.setItem("convSessionId", sid);
  }
  return sid;
}

// ---------- store ----------

let msgSeq = 0;
function nextId(): string {
  msgSeq += 1;
  return `m-${Date.now()}-${msgSeq}`;
}

interface ConversationState {
  messages: ChatMessage[];
  streaming: boolean;
  /** originMsgId returned by the backend after a card confirmation (fastpath uses it to trace the preceding script). */
  originMsgId: string | null;
  /** Topic title selected on the hot-topic card. */
  topicTitle: string | null;
  /** msgId associated with the script confirmation card. */
  scriptMsgId: string | null;
  /** The script confirmation card is rendered only once per conversation. */
  scriptCardShown: boolean;

  sendMessage: (text: string) => void;
  resume: (msgId: string) => void;
  confirmHotOption: (item: PromptItem, msgId?: string) => Promise<void>;
  confirmScriptCard: () => Promise<void>;
  clearMessages: () => void;
}

// Non-persisted runtime state (EventSource / cursor / current streaming message).
let es: EventSource | null = null;
let currentMsgId: string | null = null;
let currentSeq = 0;
let currentTextDelta = "";
let currentAssistantId: string | null = null;
let isReplaying = false;

function closeStream() {
  if (es) {
    es.close();
    es = null;
  }
}

export const useConversationStore = create<ConversationState>()(
  persist(
    (set, get) => {
      const pushMessage = (msg: ChatMessage) =>
        set((s) => ({ messages: [...s.messages, msg] }));

      const resetUi = () => set({ streaming: false });

      /** Shared SSE event handling: both sending messages and reconnect replay go through here. */
      const handleSseEvent = (name: SseEventName, data: unknown) => {
        const d = (data ?? {}) as Record<string, unknown>;
        // currentSeq is the resume cursor sent back as lastSeq: the number of backend chunks
        // already consumed for the current msgId. message-start is itself chunk #1; the synthetic
        // "reconnect" frame pushed by the attach replay is NOT a persisted chunk and must not
        // advance the cursor (otherwise the server re-delivers an already-consumed chunk).
        if (name === "message-start") {
          currentSeq = 1;
        } else if (name !== "reconnect") {
          currentSeq += 1;
        }

        switch (name) {
          case "message-start": {
            if (typeof d.msgId === "string" && d.msgId) {
              currentMsgId = d.msgId;
            }
            currentTextDelta = "";
            if (!isReplaying) {
              currentAssistantId = nextId();
              pushMessage({
                id: currentAssistantId,
                kind: "assistant",
                content: "…",
              });
            }
            break;
          }

          case "text-delta": {
            const piece =
              typeof d === "string"
                ? d
                : typeof d.content === "string"
                  ? d.content
                  : "";
            currentTextDelta += piece;
            if (currentAssistantId) {
              const id = currentAssistantId;
              set((s) => ({
                messages: s.messages.map((m) =>
                  m.id === id && m.kind === "assistant"
                    ? { ...m, content: currentTextDelta }
                    : m,
                ),
              }));
            }
            break;
          }

          case "tool-call-start":
            if (typeof d.content === "string" && d.content) {
              pushMessage({
                id: nextId(),
                kind: "tool",
                content: `🔧 ${d.content}`,
              });
            }
            break;

          case "tool-call-result":
            if (typeof d.content === "string" && d.content) {
              pushMessage({
                id: nextId(),
                kind: "tool",
                content: `✅ ${d.content}`,
              });
            }
            break;

          case "reconnect":
            if (d.interrupted) {
              pushMessage({
                id: nextId(),
                kind: "error",
                content:
                  "⚠ Generation interrupted (worker failure); the partial output is shown",
              });
              resetUi();
            } else {
              isReplaying = true;
              currentTextDelta = "";
              currentAssistantId = nextId();
              pushMessage({
                id: currentAssistantId,
                kind: "assistant",
                content: "(Completing the previous unfinished answer…)",
              });
            }
            break;

          case "interrupt":
            if (typeof d.content === "string" && d.content) {
              pushMessage({
                id: nextId(),
                kind: "hil-card",
                content: d.content,
                msgId:
                  typeof d.msgId === "string"
                    ? d.msgId
                    : (currentMsgId ?? undefined),
              });
              if (typeof d.msgId === "string" && d.msgId)
                currentMsgId = d.msgId;
              resetUi();
            }
            break;

          case "ui-block-start": {
            const uiData = d.uiData as HotCardUiData | undefined;
            if (uiData?.promptItems) {
              pushMessage({
                id: nextId(),
                kind: "hot-card",
                uiData,
                msgId:
                  typeof d.msgId === "string"
                    ? d.msgId
                    : (currentMsgId ?? undefined),
              });
            }
            break;
          }

          case "ui-block-complete":
            break;

          case "task-step": {
            if (typeof d.stepName === "string" && d.stepName) {
              let text = `🔧 Subtask: ${d.stepName}`;
              if (d.status === 1) text += " (running)";
              else if (d.status === 3) text += " (done)";
              pushMessage({ id: nextId(), kind: "tool", content: text });
            }
            break;
          }

          case "task-done": {
            const output = typeof d.output === "string" ? d.output : "";
            const reason = typeof d.reason === "string" ? d.reason : "";
            // A failed subtask arrives as task-done with status=4 (TaskRunStatusFailed) + reason and
            // no output. Surface it instead of silently ignoring it (otherwise the user is left hanging).
            if (d.status === 4 || (!output && reason)) {
              pushMessage({
                id: nextId(),
                kind: "error",
                content: `⚠ Task failed${reason ? `: ${reason}` : ""}`,
              });
              break;
            }
            if (output) {
              let finalProduct: string = output;
              try {
                const out = JSON.parse(output) as { final_product?: string };
                finalProduct = out.final_product || output;
              } catch {
                // Non-JSON output is displayed as-is
              }
              pushMessage({
                id: nextId(),
                kind: "tool",
                content: `✅ Task done: ${finalProduct}`,
              });

              // Script confirmation card: rendered when a subtask produces a script (stepName is the scriptwriter role).
              const stepName = typeof d.stepName === "string" ? d.stepName : "";
              const msgId = typeof d.msgId === "string" ? d.msgId : "";
              if (
                stepName.includes("scriptwriter") &&
                msgId &&
                !get().scriptCardShown
              ) {
                set({ scriptCardShown: true, scriptMsgId: msgId });
                pushMessage({ id: nextId(), kind: "script-card", msgId });
              }
            }
            break;
          }

          case "message-complete":
            currentMsgId = null;
            if (currentAssistantId && !currentTextDelta.trim()) {
              const id = currentAssistantId;
              set((s) => ({
                messages: s.messages.map((m) =>
                  m.id === id && m.kind === "assistant"
                    ? { ...m, content: "(empty response)" }
                    : m,
                ),
              }));
            }
            currentAssistantId = null;
            currentTextDelta = "";
            isReplaying = false;
            // Restore interactivity as soon as the message stream ends (hot-topic/HIL cards must be clickable at this point).
            resetUi();
            break;

          case "error":
            if (typeof d.content === "string" && d.content) {
              pushMessage({ id: nextId(), kind: "error", content: d.content });
            }
            break;

          default:
            console.log("SSE event:", name, data);
        }
      };

      const startStream = (params: Record<string, string | number>) => {
        closeStream();
        const claims = currentClaims();
        es = openSse(
          "/aigc/conversation",
          {
            userId: claims?.user_id ?? 1,
            sessionId: sessionId(),
            agentId: "default",
            ...params,
          },
          {
            onEvent: handleSseEvent,
            onDisconnect: () => {
              // Disconnected: if there is an unfinished msgId, auto-reconnect and replay (resume from the breakpoint).
              if (currentMsgId && currentSeq > 0) {
                set({ streaming: false });
                replay(currentMsgId, currentSeq);
              } else {
                resetUi();
              }
            },
          },
        );
      };

      /** Reconnect after a drop: resubscribe with the last msgId + lastSeq so the server replays from the breakpoint. */
      const replay = (msgId: string, lastSeq: number) => {
        if (get().streaming) return;
        set({ streaming: true });
        isReplaying = true;
        startStream({ query: "", reconnect: "1", msgId, lastSeq });
      };

      return {
        messages: [],
        streaming: false,
        originMsgId: null,
        topicTitle: null,
        scriptMsgId: null,
        scriptCardShown: false,

        sendMessage: (text) => {
          if (!text || get().streaming) return;

          set({ streaming: true });
          pushMessage({ id: nextId(), kind: "user", content: text });

          currentAssistantId = null;
          currentTextDelta = "";

          const params: Record<string, string | number> = { query: text };
          const { originMsgId } = get();
          if (originMsgId) params.originMsgId = originMsgId;
          startStream(params);
        },

        resume: (msgId: string) => {
          if (get().streaming) return;
          set({ streaming: true });
          // The query text is a protocol string consumed by the backend resume fast path (kept in Chinese).
          startStream({ query: "继续", resume: "1", msgId });
        },

        confirmHotOption: async (item, msgId) => {
          const title = item.title || "";
          const cardType = item.pipeline ? "category" : "topic";
          try {
            const resp = await api.post<ClarificationResponse>(
              "/aigc/agent/submit_clarification",
              {
                sessionId: sessionId(),
                cardType,
                value: title,
                selectedEid: item.eid || "",
                topicTitle: title,
                agentId: "idea_king",
                pipeline: item.pipeline || "",
                msgId: msgId || "",
              },
            );
            if (resp?.originMsgId) set({ originMsgId: resp.originMsgId });
            pushMessage({
              id: nextId(),
              kind: "tool",
              content: `Selected "${title}"`,
            });
            // Explicitly clear streaming so sendMessage can proceed (does not rely on message-complete timing).
            set({ streaming: false });
            // The follow-up queries are protocol strings: the backend manager prompt matches
            // these exact Chinese patterns to advance the creation flow, so they stay in Chinese.
            if (cardType === "category") {
              const topic = get().topicTitle || title;
              get().sendMessage(`根据「${topic}」用${title}开始创作`);
            } else {
              set({ topicTitle: title });
              get().sendMessage(`根据「${title}」开始创作`);
            }
          } catch (err) {
            pushMessage({
              id: nextId(),
              kind: "error",
              content: `⚠ Selection confirmation failed: ${err instanceof Error ? err.message : String(err)}`,
            });
          }
        },

        confirmScriptCard: async () => {
          const { scriptMsgId } = get();
          try {
            const resp = await api.post<ClarificationResponse>(
              "/aigc/agent/submit_clarification",
              {
                sessionId: sessionId(),
                cardType: "script",
                value: "Confirm script",
                agentId: "gold_scriptwriter",
                pipeline: "mix_video",
                msgId: scriptMsgId || "",
              },
            );
            if (resp?.originMsgId) set({ originMsgId: resp.originMsgId });
            pushMessage({
              id: nextId(),
              kind: "tool",
              content:
                "✅ Script confirmed; dispatching the video synthesis task…",
            });
            // Protocol string matched by the backend manager prompt (kept in Chinese).
            get().sendMessage("继续执行视频合成");
          } catch (err) {
            pushMessage({
              id: nextId(),
              kind: "error",
              content: `⚠ Script confirmation failed: ${err instanceof Error ? err.message : String(err)}`,
            });
          }
        },

        clearMessages: () => {
          closeStream();
          currentMsgId = null;
          currentSeq = 0;
          currentTextDelta = "";
          currentAssistantId = null;
          isReplaying = false;
          set({
            messages: [],
            streaming: false,
            originMsgId: null,
            topicTitle: null,
            scriptMsgId: null,
            scriptCardShown: false,
          });
        },
      };
    },
    {
      name: `conv:${sessionId()}`,
      partialize: (s) => ({
        messages: s.messages,
        originMsgId: s.originMsgId,
        topicTitle: s.topicTitle,
        scriptMsgId: s.scriptMsgId,
        scriptCardShown: s.scriptCardShown,
      }),
    },
  ),
);
