import { signal } from "@lit-labs/signals";
import type { ContactInfo, Message } from "../types";

export interface ChatSnapshot {
  contact: ContactInfo | null;
  sessionId: string;
  messages: Message[];
}

export const chatStore = signal<ChatSnapshot>({
  contact: null,
  sessionId: "",
  messages: [],
});

export function setChatContact(contact: ContactInfo): void {
  chatStore.set({ ...chatStore.get(), contact });
}

export function setChatSessionId(sessionId: string): void {
  chatStore.set({ ...chatStore.get(), sessionId });
}

export function addChatMessage(msg: Message): void {
  const { messages } = chatStore.get();
  chatStore.set({ ...chatStore.get(), messages: [...messages, msg] });
}

export function setChatMessages(messages: Message[]): void {
  chatStore.set({ ...chatStore.get(), messages });
}

export function clearChat(): void {
  chatStore.set({ contact: null, sessionId: "", messages: [] });
}
