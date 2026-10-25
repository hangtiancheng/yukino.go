import { create } from "zustand";

import type { MealResponse, SourceMode } from "@/lib/schemas";

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  createdAt: number;
  sessionId?: string;
  traceId?: string;
  responseType?: string;
  clarifyQuestion?: string;
  missingSlots?: string[];
  meals?: MealResponse[];
  isError?: boolean;
}

interface ChatState {
  sessionId: string | null;
  sourceMode: SourceMode;
  sending: boolean;
  messages: ChatMessage[];
  setSessionId: (sessionId: string | null) => void;
  setSourceMode: (sourceMode: SourceMode) => void;
  setSending: (sending: boolean) => void;
  addMessage: (message: ChatMessage) => void;
  clearMessages: () => void;
}

let sequence = 0;

export function nextMessageId(): string {
  sequence += 1;
  return `msg-${Date.now()}-${sequence}`;
}

export const useChatStore = create<ChatState>((set) => ({
  sessionId: null,
  sourceMode: "PERSONAL",
  sending: false,
  messages: [],
  setSessionId: (sessionId) => set({ sessionId }),
  setSourceMode: (sourceMode) => set({ sourceMode }),
  setSending: (sending) => set({ sending }),
  addMessage: (message) =>
    set((state) => ({ messages: [...state.messages, message] })),
  clearMessages: () => set({ messages: [], sessionId: null }),
}));
