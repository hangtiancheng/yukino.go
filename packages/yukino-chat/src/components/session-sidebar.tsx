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

import { customElement, state } from "@yukino.js/lit-jsx";
import { api } from "@/service/api";
import { currentUser } from "@/store/auth";
import {
  sessionStore,
  setUserSessions,
  setGroupSessions,
} from "@/store/session";
import { CollapsibleSection } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { TwElement } from "@/styles/base";
import { resolveAvatar } from "@/utils/avatar";
import type { SessionItem } from "@/types";

interface SectionProps {
  title: string;
  open: boolean;
  loading: boolean;
  query: string;
  sessions: SessionItem[];
  rowId: (s: SessionItem) => string;
  rowName: (s: SessionItem) => string;
  onToggle: () => void;
  onChat: (id: string) => void;
}

function Section({
  title,
  open,
  loading,
  query,
  sessions,
  rowId,
  rowName,
  onToggle,
  onChat,
}: SectionProps) {
  const needle = query.trim().toLowerCase();
  const visible = needle
    ? sessions.filter((s) => rowName(s).toLowerCase().includes(needle))
    : sessions;
  return (
    <CollapsibleSection
      title={title}
      count={sessions.length}
      open={open}
      onToggle={onToggle}
    >
      {loading ? (
        <p className="text-muted-foreground animate-pulse px-3 py-3 text-xs">
          Loading sessions…
        </p>
      ) : visible.length === 0 ? (
        <p className="text-muted-foreground px-3 py-3 text-xs">
          {needle ? "No matches found" : "No sessions yet"}
        </p>
      ) : (
        visible.map((session) => {
          const id = rowId(session);
          const name = rowName(session);
          return (
            <button
              key={id || name}
              type="button"
              onClick={() => onChat(id)}
              className="hover:bg-accent/60 active:bg-accent flex w-full cursor-pointer items-center gap-2.5 px-3 py-2 text-left transition-colors duration-150"
            >
              <x-avatar
                className="size-9 shrink-0"
                src={session.avatar}
                name={name}
              />
              <span className="text-foreground truncate text-sm">{name}</span>
            </button>
          );
        })
      )}
    </CollapsibleSection>
  );
}

/**
 * Session list with searchable Users / Groups collapsible sections.
 * Re-syncs from the server whenever WS system frames bump the
 * session refresh tick.
 */
@customElement("x-session-sidebar")
export class XSessionSidebar extends TwElement {
  @state() private query = "";
  @state() private usersOpen = true;
  @state() private groupsOpen = false;
  @state() private usersLoading = false;
  @state() private groupsLoading = false;
  private groupsLoaded = false;
  private lastTick = -1;
  onChat?: (id: string) => void;

  override connectedCallback() {
    super.connectedCallback();
    void this.loadUserSessions();
  }

  protected override updated() {
    const tick = sessionStore.get().refreshTick;
    if (tick !== this.lastTick) {
      this.lastTick = tick;
      void this.loadUserSessions();
      if (this.groupsLoaded) void this.loadGroupSessions();
    }
  }

  async loadUserSessions() {
    const uid = currentUser().uuid;
    if (!uid) return;
    this.usersLoading = true;
    try {
      const res = await api.getUserSessionList({ owner_id: uid });
      if (res.code === 200) {
        const list = ((res.data as SessionItem[]) || []).map((u) => ({
          ...u,
          avatar: resolveAvatar(u.avatar, u.user_id),
        }));
        setUserSessions(list);
      }
    } finally {
      this.usersLoading = false;
    }
  }

  async loadGroupSessions() {
    const uid = currentUser().uuid;
    if (!uid) return;
    this.groupsLoading = true;
    try {
      const res = await api.getGroupSessionList({ owner_id: uid });
      if (res.code === 200) {
        const list = ((res.data as SessionItem[]) || []).map((g) => ({
          ...g,
          avatar: resolveAvatar(g.avatar, g.group_id),
        }));
        setGroupSessions(list);
      }
    } finally {
      this.groupsLoading = false;
    }
  }

  override render() {
    const s = sessionStore.get();
    return (
      <div className="flex h-full w-full flex-col">
        <div className="p-2">
          <Input
            value={this.query}
            placeholder="Search sessions"
            ariaLabel="Search sessions"
            onValue={(v) => (this.query = v)}
            className="h-8"
          />
        </div>

        <div className="nice-scroll flex-1 overflow-y-auto">
          <Section
            title="Users"
            open={this.usersOpen}
            loading={this.usersLoading}
            query={this.query}
            sessions={s.userSessions}
            rowId={(u) => u.user_id ?? ""}
            rowName={(u) => u.username ?? ""}
            onToggle={() => (this.usersOpen = !this.usersOpen)}
            onChat={(id) => this.onChat?.(id)}
          />
          <Section
            title="Groups"
            open={this.groupsOpen}
            loading={this.groupsLoading}
            query={this.query}
            sessions={s.groupSessions}
            rowId={(g) => g.group_id ?? ""}
            rowName={(g) => g.group_name ?? ""}
            onToggle={() => {
              this.groupsOpen = !this.groupsOpen;
              if (this.groupsOpen && !this.groupsLoaded) {
                this.groupsLoaded = true;
                void this.loadGroupSessions();
              }
            }}
            onChat={(id) => this.onChat?.(id)}
          />
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-session-sidebar": XSessionSidebar;
  }
}
