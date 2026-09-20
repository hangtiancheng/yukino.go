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

import {
  createRef,
  customElement,
  property,
  query,
  state,
} from "@yukino.js/lit-jsx";
import { api } from "@/service/api";
import { currentUser } from "@/store/auth";
import {
  chatStore,
  setChatContact,
  setChatSessionId,
  setChatMessages,
  addChatMessage,
  clearChat,
} from "@/store/chat";
import { bumpSessionRefresh } from "@/store/session";
import { sendWs, setWsHandler } from "@/store/ws";
import { resolveAvatar } from "@/utils/avatar";
import { showToast } from "@/utils/toast";
import { performLogout } from "@/utils/logout";
import { getFileSize } from "@/utils/format";
import { navigate } from "@/router";
import { BASE_URL } from "@/config";
import { TwElement } from "@/styles/base";
import { AppFrame } from "@/components/app-frame";
import "@/components/session-sidebar";
import { MessageList } from "@/components/message-bubble";
import { XVideoCall } from "@/components/video-call";
import "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea } from "@/components/ui/input";
import {
  DialogFooter,
  DialogHeader,
  DialogTitle,
  InfoRow,
} from "@/components/ui/dialog";
import { MenuItem, MenuSeparator, XMenu } from "@/components/ui/menu";
import { Checkbox, RadioGroup } from "@/components/ui/collapsible";
import { icon, icons } from "@/components/icons";
import type { ContactInfo, Message } from "@/types";

interface MemberRow {
  user_id: string;
  nickname: string;
  avatar: string;
}

interface JoinRequestRow {
  apply_id: string;
  user_id: string;
  contact_id: string;
  contact_name: string;
  contact_type: number;
  status: number;
  message: string;
}

@customElement("sc-chat")
export class ChatPage extends TwElement {
  @property() contactId = "";

  @state() private chatMessage = "";
  @state() private memberList: MemberRow[] = [];
  @state() private joinRequestList: JoinRequestRow[] = [];
  @state() private selectedMembers: string[] = [];

  // Edit group modal buffers
  @state() private editGroupName = "";
  @state() private editGroupNotice = "";
  @state() private editGroupAddMode = -1;
  @state() private groupAvatarFile: File | null = null;

  // Dialog open states
  @state() private userInfoOpen = false;
  @state() private groupInfoOpen = false;
  @state() private editGroupOpen = false;
  @state() private removeMembersOpen = false;
  @state() private joinRequestsOpen = false;

  private videoRef = createRef<XVideoCall>();
  private lastMessageCount = 0;

  @query("#chat-messages") private messagesEl!: HTMLDivElement;

  protected override willUpdate(changed: Map<string, unknown>) {
    if (changed.has("contactId")) {
      clearChat();
      this.lastMessageCount = 0;
      if (this.contactId) void this.loadChat(this.contactId);
    }
  }

  protected override updated() {
    const count = chatStore.get().messages.length;
    if (count !== this.lastMessageCount) {
      this.lastMessageCount = count;
      this.scrollToBottom();
    }
  }

  override connectedCallback() {
    super.connectedCallback();
    setWsHandler(this.handleWsMessage);
  }

  override disconnectedCallback() {
    setWsHandler(null);
    super.disconnectedCallback();
  }

  private scrollToBottom() {
    if (this.messagesEl)
      this.messagesEl.scrollTop = this.messagesEl.scrollHeight;
  }

  private async loadMessages(cid: string) {
    const user = currentUser();
    const isUser = cid.startsWith("U");
    const res = isUser
      ? await api.getMessageList({ send_id: user.uuid, receive_id: cid })
      : await api.getGroupMessageList({ group_id: cid });
    if (res.code === 200 && res.data) {
      const list = ((res.data as Message[]) || []).map((m) => ({
        ...m,
        send_avatar: resolveAvatar(m.send_avatar, m.send_id),
      }));
      setChatMessages(list);
    }
  }

  private async loadChat(cid: string) {
    const user = currentUser();
    const res = await api.getContactInfo({
      user_id: user.uuid,
      contact_id: cid,
    });
    if (res.code !== 200 || !res.data) return;
    const info = res.data as ContactInfo;
    info.contact_avatar = resolveAvatar(info.contact_avatar, info.contact_id);
    setChatContact(info);

    const sessionRes = await api.openSession({
      send_id: user.uuid,
      receive_id: cid,
    });
    if (sessionRes.code === 200) {
      setChatSessionId(sessionRes.data as string);
      await this.loadMessages(cid);
    }
  }

  private handleWsMessage = (event: MessageEvent) => {
    const raw = event.data;
    if (typeof raw !== "string" || raw.length === 0 || raw.charAt(0) !== "{")
      return;
    let message: Message;
    try {
      message = JSON.parse(raw) as Message;
    } catch {
      return;
    }
    const user = currentUser();
    const chat = chatStore.get();

    if (message.type === 5) {
      // System notification: contact/group/session state changed elsewhere.
      bumpSessionRefresh();
      return;
    }

    if (message.type === 3) {
      try {
        const avData = JSON.parse(message.av_data || "{}") as Record<
          string,
          unknown
        >;
        if (avData.type === "call_failed") {
          showToast(
            `Call failed: ${(avData.reason as string) || "unknown reason"}`,
            "error",
          );
          return;
        }
        this.videoRef.value?.handleSignal(avData);
      } catch {
        /* ignore malformed signal */
      }
      return;
    }

    const currentContactId = chat.contact?.contact_id;
    const isRelevant =
      (message.receive_id.startsWith("G") &&
        message.receive_id === currentContactId) ||
      (message.receive_id.startsWith("U") &&
        message.receive_id === user.uuid &&
        message.send_id === currentContactId) ||
      (message.send_id === user.uuid &&
        message.receive_id === currentContactId);

    if (isRelevant) {
      message.send_avatar = resolveAvatar(message.send_avatar, message.send_id);
      addChatMessage(message);
    }
  };

  private sendMessage() {
    const chat = chatStore.get();
    const user = currentUser();
    if (!this.chatMessage.trim() || !chat.contact) return;
    const msg: Message = {
      session_id: chat.sessionId,
      type: 0,
      content: this.chatMessage,
      url: "",
      send_id: user.uuid,
      send_name: user.nickname,
      send_avatar: user.avatar,
      receive_id: chat.contact.contact_id,
      file_size: getFileSize(0),
      file_name: "",
      file_type: "",
      created_at: new Date().toISOString(),
    };
    sendWs(msg);
    this.chatMessage = "";
  }

  private async onFileSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    const chat = chatStore.get();
    const user = currentUser();
    const formData = new FormData();
    formData.append("file", file);
    const res = await api.uploadFile(formData);
    const data = res.data as { url?: string; file_name?: string } | null;
    const uploadedUrl = data?.url || "";
    const uploadedName = data?.file_name || file.name;
    if (!uploadedUrl) {
      showToast("File upload failed", "error");
      return;
    }
    showToast("File uploaded successfully", "success");
    input.value = "";
    if (!chat.contact) return;
    const msg: Message = {
      session_id: chat.sessionId,
      type: 2,
      content: "",
      url: BASE_URL + uploadedUrl,
      send_id: user.uuid,
      send_name: user.nickname,
      send_avatar: user.avatar,
      receive_id: chat.contact.contact_id,
      file_size: getFileSize(file.size),
      file_name: uploadedName,
      file_type: file.type,
      created_at: new Date().toISOString(),
    };
    sendWs(msg);
  }

  private async deleteSession() {
    const chat = chatStore.get();
    await api.deleteSession({
      owner_id: currentUser().uuid,
      session_id: chat.sessionId,
    });
    navigate("/chat/sessions");
  }

  private async deleteContact() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    await api.deleteContact({
      user_id: currentUser().uuid,
      contact_id: chat.contact.contact_id,
    });
    showToast("Contact removed", "success");
    navigate("/chat/sessions");
  }

  private async blackContact() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    await api.blackContact({
      user_id: currentUser().uuid,
      contact_id: chat.contact.contact_id,
    });
    showToast("Contact blocked", "success");
    navigate("/chat/sessions");
  }

  private async dismissGroup() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    const res = await api.dismissGroup({ group_id: chat.contact.contact_id });
    if (res.code === 200) {
      showToast("Group disbanded", "success");
      navigate("/chat/sessions");
    } else {
      showToast(res.message, "error");
    }
  }

  private async leaveGroup() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    const res = await api.leaveGroup({
      user_id: currentUser().uuid,
      group_id: chat.contact.contact_id,
    });
    if (res.code === 200) {
      showToast("Left group", "success");
      navigate("/chat/sessions");
    } else {
      showToast(res.message, "error");
    }
  }

  private showEditGroupModal() {
    this.editGroupName = "";
    this.editGroupNotice = "";
    this.editGroupAddMode = -1;
    this.groupAvatarFile = null;
    this.editGroupOpen = true;
  }

  private async saveGroupInfo() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    if (
      !this.editGroupName &&
      !this.editGroupNotice &&
      this.editGroupAddMode === -1 &&
      !this.groupAvatarFile
    ) {
      showToast("Please modify at least one field", "warning");
      return;
    }
    if (
      this.editGroupName &&
      (this.editGroupName.length < 3 || this.editGroupName.length > 10)
    ) {
      showToast("Group name must be 3-10 characters", "error");
      return;
    }
    let avatarUrl = "";
    if (this.groupAvatarFile) {
      const formData = new FormData();
      formData.append("file", this.groupAvatarFile);
      const uploadRes = await api.uploadAvatar(formData);
      avatarUrl = (uploadRes.data as { url?: string } | null)?.url ?? "";
      if (uploadRes.code !== 200 || !avatarUrl) {
        showToast(uploadRes.message || "Avatar upload failed", "error");
        return;
      }
    }
    const data: Record<string, unknown> = { uuid: chat.contact.contact_id };
    if (this.editGroupName) data.name = this.editGroupName;
    if (this.editGroupNotice) data.notice = this.editGroupNotice;
    if (this.editGroupAddMode !== -1) data.add_mode = this.editGroupAddMode;
    if (avatarUrl) data.avatar = avatarUrl;
    const res = await api.updateGroupInfo(data);
    if (res.code === 200) {
      showToast("Group updated", "success");
      this.editGroupOpen = false;
      void this.loadChat(chat.contact.contact_id);
    } else {
      showToast(res.message, "error");
    }
  }

  private async showRemoveMembersModal() {
    const chat = chatStore.get();
    if (!chat.contact) return;
    this.selectedMembers = [];
    const res = await api.getGroupMemberList({
      group_id: chat.contact.contact_id,
    });
    const list = ((res.data as MemberRow[]) || []).map((m) => ({
      ...m,
      avatar: resolveAvatar(m.avatar, m.user_id),
    }));
    this.memberList = list;
    this.removeMembersOpen = true;
  }

  private toggleMember(mid: string, checked: boolean) {
    this.selectedMembers = checked
      ? [...this.selectedMembers, mid]
      : this.selectedMembers.filter((x) => x !== mid);
  }

  private async removeSelectedMembers() {
    const chat = chatStore.get();
    if (this.selectedMembers.length === 0) {
      showToast("Please select members to remove", "warning");
      return;
    }
    if (!chat.contact) return;
    const res = await api.removeGroupMembers({
      group_id: chat.contact.contact_id,
      member_ids: this.selectedMembers,
    });
    if (res.code === 200) {
      showToast("Members removed", "success");
      this.memberList = this.memberList.filter(
        (m) => !this.selectedMembers.includes(m.user_id),
      );
      this.selectedMembers = [];
    } else {
      showToast(res.message, "error");
    }
  }

  private async showJoinRequestsModal() {
    const res = await api.getAddGroupList({ user_id: currentUser().uuid });
    const list = ((res.data as JoinRequestRow[]) || []).filter(
      (r) => r.contact_type === 1 && r.status === 0,
    );
    if (list.length === 0) {
      showToast("No pending join requests", "info");
      return;
    }
    this.joinRequestList = list;
    this.joinRequestsOpen = true;
  }

  private async approveJoinRequest(applyId: string) {
    const res = await api.passContactApply({ apply_id: applyId });
    if (res.code === 200) {
      showToast("Approved", "success");
      this.joinRequestList = this.joinRequestList.filter(
        (r) => r.apply_id !== applyId,
      );
    } else {
      showToast(res.message, "error");
    }
  }

  private async rejectJoinRequest(applyId: string) {
    const res = await api.refuseContactApply({ apply_id: applyId });
    if (res.code === 200) {
      showToast("Rejected", "success");
      this.joinRequestList = this.joinRequestList.filter(
        (r) => r.apply_id !== applyId,
      );
    } else {
      showToast(res.message, "error");
    }
  }

  override render() {
    const chat = chatStore.get();
    const user = currentUser();
    const contact = chat.contact;
    const contactId = contact?.contact_id ?? "";
    const contactName = contact?.contact_name ?? "";
    const contactAvatar = contact?.contact_avatar ?? "";
    const isUserContact = contactId.startsWith("U");
    const isGroupContact = contactId !== "" && !isUserContact;
    const isGroupOwner = contact?.contact_owner_id === user.uuid;
    const contactGenderText =
      isUserContact && contact
        ? contact.contact_gender === 0
          ? "Male"
          : "Female"
        : "";
    const groupMemberCnt = isGroupContact
      ? (contact?.contact_member_cnt ?? 0)
      : 0;
    const groupOwnerId = contact?.contact_owner_id ?? "";
    const groupAddModeText =
      isGroupContact && contact
        ? contact.contact_add_mode === 0
          ? "Direct Join"
          : "Owner Approval"
        : "";

    return (
      <AppFrame
        active="/chat/sessions"
        onLogout={async () => {
          await performLogout();
          navigate("/login");
        }}
        sidebar={
          <x-session-sidebar
            onChat={(cid: string) => navigate(`/chat/${cid}`)}
          />
        }
      >
        {/* Header */}
        <div className="border-border bg-primary/5 flex h-14 shrink-0 items-center justify-between border-b px-4">
          <div className="flex min-w-0 items-center gap-3">
            {contactAvatar && (
              <x-avatar
                className="ring-primary/30 size-10 shrink-0 ring-2"
                src={contactAvatar}
                name={contactName}
              />
            )}
            <h2 className="text-foreground truncate text-base font-semibold">
              {contactName || "Select a conversation"}
            </h2>
          </div>
          {contact && (
            <XMenu align="end">
              <span slot="trigger">
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-muted-foreground"
                  ariaLabel="Chat options"
                >
                  {icon(icons.EllipsisVertical, "size-4")}
                </Button>
              </span>
              {isUserContact && (
                <MenuItem onClick={() => (this.userInfoOpen = true)}>
                  User Info
                </MenuItem>
              )}
              {isGroupContact && (
                <MenuItem onClick={() => (this.groupInfoOpen = true)}>
                  Group Info
                </MenuItem>
              )}
              {isGroupContact && isGroupOwner && (
                <>
                  <MenuItem onClick={() => this.showEditGroupModal()}>
                    Edit Group
                  </MenuItem>
                  <MenuItem onClick={() => this.showRemoveMembersModal()}>
                    Remove Members
                  </MenuItem>
                  <MenuItem onClick={() => this.showJoinRequestsModal()}>
                    Join Requests
                  </MenuItem>
                </>
              )}
              <MenuSeparator />
              <MenuItem onClick={() => this.deleteSession()}>
                Delete Session
              </MenuItem>
              {isUserContact && (
                <>
                  <MenuItem onClick={() => this.deleteContact()}>
                    Remove Contact
                  </MenuItem>
                  <MenuItem destructive onClick={() => this.blackContact()}>
                    Block Contact
                  </MenuItem>
                </>
              )}
              {isGroupContact &&
                (isGroupOwner ? (
                  <MenuItem destructive onClick={() => this.dismissGroup()}>
                    Disband Group
                  </MenuItem>
                ) : (
                  <MenuItem onClick={() => this.leaveGroup()}>
                    Leave Group
                  </MenuItem>
                ))}
            </XMenu>
          )}
        </div>

        {/* Messages */}
        <div
          id="chat-messages"
          className="bg-muted/20 nice-scroll flex flex-1 flex-col overflow-y-auto p-4"
        >
          <MessageList
            messages={chat.messages}
            currentUserId={user.uuid}
            currentUserAvatar={user.avatar}
            currentUserName={user.nickname}
          />
        </div>

        {/* Toolbar */}
        <div className="border-border bg-muted/30 flex h-10 shrink-0 items-center justify-between gap-1 border-t px-2">
          <label className="cursor-pointer">
            <input
              type="file"
              className="hidden"
              onChange={(e: Event) => this.onFileSelect(e)}
            />
            <span className="text-muted-foreground hover:bg-accent hover:text-accent-foreground flex size-8 items-center justify-center rounded-md transition-all duration-200">
              {icon(icons.Paperclip, "size-4")}
            </span>
          </label>
          <Button
            variant="ghost"
            size="icon"
            className="text-muted-foreground"
            ariaLabel="Video call"
            onClick={() => this.videoRef.value?.show()}
          >
            {icon(icons.Video, "size-4")}
          </Button>
        </div>

        <x-video-call ref={this.videoRef}></x-video-call>

        {/* Composer */}
        <div className="border-border flex h-40 shrink-0 border-t">
          <Textarea
            className="bg-card placeholder:text-muted-foreground/50 h-full flex-1 rounded-none border-0 focus-visible:ring-0"
            placeholder="Type a message… Enter to send, Shift+Enter for a new line"
            maxLength={500}
            value={this.chatMessage}
            onValue={(v) => (this.chatMessage = v)}
            onKeyDown={(e: KeyboardEvent) => {
              const target = e.target as HTMLTextAreaElement;
              if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
                e.preventDefault();
                if (target.value.trim()) this.sendMessage();
              }
            }}
          />
          <div className="flex w-20 flex-col justify-end p-2">
            <Button
              className="h-10"
              disabled={!this.chatMessage.trim() || !contact}
              onClick={() => this.sendMessage()}
            >
              {icon(icons.Send, "size-4")}
            </Button>
          </div>
        </div>

        {/* User Info Dialog */}
        <x-dialog
          open={this.userInfoOpen}
          onClose={() => (this.userInfoOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>User Profile</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col text-sm">
            <InfoRow label="ID" value={contactId} />
            <InfoRow label="Name" value={contactName} />
            <InfoRow label="Gender" value={contactGenderText} />
            <InfoRow label="Phone" value={contact?.contact_phone ?? ""} />
            <InfoRow label="Email" value={contact?.contact_email ?? ""} />
            <InfoRow label="Birthday" value={contact?.contact_birthday ?? ""} />
            <div className="py-1.5">
              <span className="text-muted-foreground">Signature</span>
              <p className="text-foreground mt-1">
                {contact?.contact_signature ?? ""}
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => (this.userInfoOpen = false)}
            >
              Close
            </Button>
          </DialogFooter>
        </x-dialog>

        {/* Group Info Dialog */}
        <x-dialog
          open={this.groupInfoOpen}
          onClose={() => (this.groupInfoOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Group Info</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col text-sm">
            <InfoRow label="ID" value={contactId} />
            <InfoRow label="Name" value={contactName} />
            <InfoRow label="Members" value={groupMemberCnt} />
            <InfoRow label="Owner" value={groupOwnerId} />
            <InfoRow label="Join Mode" value={groupAddModeText} />
            <div className="py-1.5">
              <span className="text-muted-foreground">Notice</span>
              <p className="text-foreground mt-1">
                {contact?.contact_notice ?? ""}
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => (this.groupInfoOpen = false)}
            >
              Close
            </Button>
          </DialogFooter>
        </x-dialog>

        {/* Edit Group Dialog */}
        <x-dialog
          open={this.editGroupOpen}
          onClose={() => (this.editGroupOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Edit Group</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="edit-group-name">Group Name</Label>
              <Input
                id="edit-group-name"
                placeholder="3-10 characters"
                value={this.editGroupName}
                onValue={(v) => (this.editGroupName = v)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="edit-group-notice">Notice</Label>
              <Textarea
                id="edit-group-notice"
                rows={3}
                placeholder="Optional"
                maxLength={500}
                value={this.editGroupNotice}
                onValue={(v) => (this.editGroupNotice = v)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>Join Mode</Label>
              <RadioGroup
                name="edit-addmode"
                value={
                  this.editGroupAddMode === -1
                    ? ""
                    : String(this.editGroupAddMode)
                }
                options={[
                  { value: "0", label: "Direct Join" },
                  { value: "1", label: "Owner Approval" },
                ]}
                onValueChange={(v) => (this.editGroupAddMode = Number(v))}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="edit-group-avatar">Avatar</Label>
              <Input
                id="edit-group-avatar"
                type="file"
                accept="image/*"
                onChange={(e: Event) => {
                  const input = e.target as HTMLInputElement;
                  this.groupAvatarFile = input.files?.[0] ?? null;
                }}
              />
            </div>
          </div>
          <DialogFooter>
            <Button size="sm" onClick={() => this.saveGroupInfo()}>
              Save
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (this.editGroupOpen = false)}
            >
              Cancel
            </Button>
          </DialogFooter>
        </x-dialog>

        {/* Remove Members Dialog */}
        <x-dialog
          open={this.removeMembersOpen}
          onClose={() => (this.removeMembersOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Remove Group Members</DialogTitle>
          </DialogHeader>
          {this.memberList.length === 0 && (
            <p className="text-muted-foreground py-4 text-center text-sm">
              No members found
            </p>
          )}
          <div className="nice-scroll flex max-h-60 flex-col overflow-y-auto">
            {this.memberList.map((m) => (
              <div
                key={m.user_id}
                className="border-border hover:bg-accent/50 flex cursor-pointer items-center justify-between rounded-md border-b px-2 py-2 transition-colors"
                onClick={() =>
                  this.toggleMember(
                    m.user_id,
                    !this.selectedMembers.includes(m.user_id),
                  )
                }
              >
                <div className="flex items-center gap-2">
                  <x-avatar
                    className="size-8"
                    src={m.avatar}
                    name={m.nickname}
                  />
                  <span className="text-foreground text-sm">{m.nickname}</span>
                </div>
                <Checkbox
                  checked={this.selectedMembers.includes(m.user_id)}
                  ariaLabel={`Select ${m.nickname}`}
                  onCheckedChange={(checked) =>
                    this.toggleMember(m.user_id, checked)
                  }
                />
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button
              variant="destructive"
              size="sm"
              onClick={() => this.removeSelectedMembers()}
            >
              Remove Selected
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (this.removeMembersOpen = false)}
            >
              Cancel
            </Button>
          </DialogFooter>
        </x-dialog>

        {/* Join Requests Dialog */}
        <x-dialog
          open={this.joinRequestsOpen}
          onClose={() => (this.joinRequestsOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Group Join Requests</DialogTitle>
          </DialogHeader>
          {this.joinRequestList.length === 0 && (
            <p className="text-muted-foreground py-4 text-center text-sm">
              No pending requests
            </p>
          )}
          <div className="nice-scroll flex max-h-60 flex-col gap-2 overflow-y-auto">
            {this.joinRequestList.map((req) => (
              <div
                key={req.apply_id}
                className="border-border flex items-center justify-between gap-2 border-b py-2"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm">{req.contact_name}</span>
                  {req.message && (
                    <span className="text-muted-foreground truncate text-xs">
                      ({req.message})
                    </span>
                  )}
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button
                    size="xs"
                    onClick={() => this.approveJoinRequest(req.apply_id)}
                  >
                    Approve
                  </Button>
                  <Button
                    size="xs"
                    variant="ghost"
                    className="text-muted-foreground"
                    onClick={() => this.rejectJoinRequest(req.apply_id)}
                  >
                    Reject
                  </Button>
                </div>
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => (this.joinRequestsOpen = false)}
            >
              Close
            </Button>
          </DialogFooter>
        </x-dialog>
      </AppFrame>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-chat": ChatPage;
  }
}
