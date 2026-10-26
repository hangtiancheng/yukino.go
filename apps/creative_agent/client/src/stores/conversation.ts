import { create } from "zustand";
import { persist } from "zustand/middleware";

import { api, openSse, type SseEventName } from "@/lib/api";
import { currentClaims } from "@/stores/auth";

// ---------- 类型 ----------

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

// ---------- sessionId（sessionStorage，按标签页隔离） ----------

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
  /** 点卡确认后后端返回的 originMsgId（fastpath 追溯前序脚本用）。 */
  originMsgId: string | null;
  /** 热点卡选中的选题标题。 */
  topicTitle: string | null;
  /** 脚本确认卡关联的 msgId。 */
  scriptMsgId: string | null;
  /** 同一会话只渲染一次脚本确认卡。 */
  scriptCardShown: boolean;

  sendMessage: (text: string) => void;
  resume: (msgId: string) => void;
  confirmHotOption: (item: PromptItem, msgId?: string) => Promise<void>;
  confirmScriptCard: () => Promise<void>;
  clearMessages: () => void;
}

// 非持久化的运行时状态（EventSource / 游标 / 当前流式消息）。
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

      /** 共享的 SSE 事件处理：发消息与重连回放都走这里。 */
      const handleSseEvent = (name: SseEventName, data: unknown) => {
        const d = (data ?? {}) as Record<string, unknown>;
        // message-start 会重置游标；其余帧每收到一个就 +1，保证与后端 chunk 序号对齐。
        if (name !== "message-start") currentSeq += 1;

        switch (name) {
          case "message-start": {
            if (typeof d.msgId === "string" && d.msgId) {
              currentMsgId = d.msgId;
            }
            currentSeq = 0;
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
                content: "⚠ 生成中断（Worker 异常），已显示已生成的部分",
              });
              resetUi();
            } else {
              isReplaying = true;
              currentAssistantId = nextId();
              pushMessage({
                id: currentAssistantId,
                kind: "assistant",
                content: "（正在补全上次未完成的回答…）",
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
              let text = `🔧 子任务: ${d.stepName}`;
              if (d.status === 1) text += "（执行中）";
              else if (d.status === 3) text += "（完成）";
              pushMessage({ id: nextId(), kind: "tool", content: text });
            }
            break;
          }

          case "task-done": {
            if (typeof d.output === "string" && d.output) {
              let finalProduct: string = d.output;
              try {
                const out = JSON.parse(d.output) as { final_product?: string };
                finalProduct = out.final_product || d.output;
              } catch {
                // 非 JSON 输出直接展示
              }
              pushMessage({
                id: nextId(),
                kind: "tool",
                content: `✅ 任务完成: ${finalProduct}`,
              });

              // 脚本确认卡：子任务产出脚本（stepName 为脚本角色）时渲染确认按钮。
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
                    ? { ...m, content: "(空响应)" }
                    : m,
                ),
              }));
            }
            currentAssistantId = null;
            currentTextDelta = "";
            isReplaying = false;
            // 消息流结束立即恢复可交互（热点卡/HIL 卡需要此时可点击）。
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
              // 断线：若当前有未完成的 msgId，自动重连回放（断点续拉）。
              if (currentMsgId && currentSeq > 0) {
                set({ streaming: false });
                replay(currentMsgId, currentSeq);
              } else {
                resetUi();
              }
            },
            onClose: () => resetUi(),
          },
        );
      };

      /** 断线重连：携带上次的 msgId + lastSeq 重新订阅，服务端从断点续拉回放。 */
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
              content: `已选择「${title}」`,
            });
            // 显式恢复 streaming，确保 sendMessage 能发（不依赖 message-complete 时机）。
            set({ streaming: false });
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
              content: `⚠ 选择确认失败: ${err instanceof Error ? err.message : String(err)}`,
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
                value: "确认脚本",
                agentId: "gold_scriptwriter",
                pipeline: "mix_video",
                msgId: scriptMsgId || "",
              },
            );
            if (resp?.originMsgId) set({ originMsgId: resp.originMsgId });
            pushMessage({
              id: nextId(),
              kind: "tool",
              content: "✅ 脚本已确认，正在投递视频合成任务…",
            });
            get().sendMessage("继续执行视频合成");
          } catch (err) {
            pushMessage({
              id: nextId(),
              kind: "error",
              content: `⚠ 脚本确认失败: ${err instanceof Error ? err.message : String(err)}`,
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
