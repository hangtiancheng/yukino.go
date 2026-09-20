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
import { showToast } from "@/utils/toast";
import { TwElement } from "@/styles/base";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea } from "@/components/ui/input";
import { CollapsibleSection, RadioGroup } from "@/components/ui/collapsible";
import {
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { MenuItem, XMenu } from "@/components/ui/menu";
import { icon, icons } from "@/components/icons";

interface ContactEntry {
  user_id: string;
  nickname: string;
  avatar: string;
  status: number; // 0 normal, 1 blocked by me, 2 blocked me
}

interface GroupEntry {
  group_id: string;
  name: string;
  member_cnt: number;
  owner_id: string;
  avatar: string;
}

interface RequestEntry {
  apply_id: string;
  user_id: string;
  contact_id: string;
  contact_name: string;
  contact_type: number;
  status: number;
  message: string;
}

@customElement("x-contact-sidebar")
export class XContactSidebar extends TwElement {
  @state() private friendList: ContactEntry[] = [];
  @state() private myGroupList: GroupEntry[] = [];
  @state() private joinedGroupList: GroupEntry[] = [];
  @state() private requestList: RequestEntry[] = [];

  @state() private friendsOpen = true;
  @state() private myGroupsOpen = false;
  @state() private joinedGroupsOpen = false;

  @state() private applyOpen = false;
  @state() private createGroupOpen = false;
  @state() private requestsOpen = false;

  @state() private applyId = "";
  @state() private applyMsg = "";
  @state() private groupName = "";
  @state() private groupAddMode = 0;

  private friendsLoaded = false;
  private myGroupsLoaded = false;
  private joinedGroupsLoaded = false;
  onNavigate?: (contactId: string) => void;

  override connectedCallback() {
    super.connectedCallback();
    void this.loadFriends();
  }

  private async loadFriends() {
    if (this.friendsLoaded) return;
    this.friendsLoaded = true;
    const uid = currentUser().uuid;
    const res = await api.getUserList({ owner_id: uid });
    if (res.code === 200 && res.data) {
      this.friendList = (res.data as ContactEntry[]) || [];
    }
  }

  private async loadMyGroups() {
    if (this.myGroupsLoaded) return;
    this.myGroupsLoaded = true;
    const uid = currentUser().uuid;
    const res = await api.loadMyGroup({ owner_id: uid });
    if (res.code === 200 && res.data) {
      this.myGroupList = (res.data as GroupEntry[]) || [];
    }
  }

  private async loadJoinedGroups() {
    if (this.joinedGroupsLoaded) return;
    this.joinedGroupsLoaded = true;
    const uid = currentUser().uuid;
    const res = await api.loadMyJoinedGroup({ owner_id: uid });
    if (res.code === 200 && res.data) {
      this.joinedGroupList = (res.data as GroupEntry[]) || [];
    }
  }

  private async tryOpenChat(contactId: string) {
    const uid = currentUser().uuid;
    const res = await api.checkOpenSessionAllowed({
      send_id: uid,
      receive_id: contactId,
    });
    if (res.code === 200 && res.data === true) {
      this.onNavigate?.(contactId);
    } else {
      showToast((res.message as string) || "Cannot open session", "warning");
    }
  }

  private async unblockUser(contactId: string) {
    const uid = currentUser().uuid;
    const res = await api.cancelBlackContact({
      user_id: uid,
      contact_id: contactId,
    });
    if (res.code === 200) {
      showToast("Contact unblocked", "success");
      this.friendList = this.friendList.map((u) =>
        u.user_id === contactId ? { ...u, status: 0 } : u,
      );
    } else {
      showToast((res.message as string) || "Failed to unblock", "error");
    }
  }

  private async submitApply() {
    if (!this.applyId) {
      showToast("Please enter an ID", "error");
      return;
    }
    const uid = currentUser().uuid;
    const isGroup = this.applyId.startsWith("G");

    if (isGroup) {
      const modeRes = await api.checkGroupAddMode({ group_id: this.applyId });
      if (modeRes.code === 200 && modeRes.data === 0) {
        const res = await api.enterGroupDirectly({
          user_id: uid,
          group_id: this.applyId,
        });
        if (res.code === 200) {
          showToast("Joined group", "success");
          this.applyOpen = false;
        } else {
          showToast(res.message as string, "error");
        }
      } else {
        const res = await api.applyContact({
          user_id: uid,
          contact_id: this.applyId,
          contact_type: 1,
          message: this.applyMsg,
        });
        if (res.code === 200) {
          showToast("Application sent", "success");
          this.applyOpen = false;
        } else {
          showToast(res.message as string, "error");
        }
      }
    } else {
      const res = await api.applyContact({
        user_id: uid,
        contact_id: this.applyId,
        contact_type: 0,
        message: this.applyMsg,
      });
      if (res.code === 200) {
        showToast("Application sent", "success");
        this.applyOpen = false;
      } else {
        showToast(res.message as string, "error");
      }
    }
  }

  private async submitCreateGroup() {
    if (!this.groupName) {
      showToast("Please enter a group name", "error");
      return;
    }
    const uid = currentUser().uuid;
    const res = await api.createGroup({
      name: this.groupName,
      owner_id: uid,
      avatar: "",
      add_mode: this.groupAddMode,
    });
    if (res.code === 200) {
      showToast("Group created", "success");
      this.createGroupOpen = false;
    } else {
      showToast(res.message as string, "error");
    }
  }

  private async showRequests() {
    const uid = currentUser().uuid;
    const res = await api.getNewContactList({ user_id: uid });
    const list = (res.data as RequestEntry[] | null) || [];
    if (list.length === 0) {
      showToast("No pending friend requests", "info");
      return;
    }
    this.requestList = list;
    this.requestsOpen = true;
  }

  private removeRequest(id: string) {
    this.requestList = this.requestList.filter((r) => r.apply_id !== id);
  }

  private async approveRequest(id: string) {
    const res = await api.passContactApply({ apply_id: id });
    if (res.code === 200) {
      showToast("Approved", "success");
      this.removeRequest(id);
    } else {
      showToast(res.message as string, "error");
    }
  }

  private async refuseRequest(id: string) {
    const res = await api.refuseContactApply({ apply_id: id });
    if (res.code === 200) {
      showToast("Refused", "success");
      this.removeRequest(id);
    } else {
      showToast(res.message as string, "error");
    }
  }

  private async blockRequest(id: string) {
    const res = await api.blackApply({ apply_id: id });
    if (res.code === 200) {
      showToast("Blocked", "success");
      this.removeRequest(id);
    } else {
      showToast(res.message as string, "error");
    }
  }

  override render() {
    return (
      <div className="flex h-full w-full flex-col">
        <div className="flex items-center gap-1 p-2">
          <Input
            type="text"
            className="h-8 flex-1"
            placeholder="Search contacts"
            ariaLabel="Search contacts"
          />
          <XMenu align="end">
            <span slot="trigger">
              <Button
                variant="outline"
                size="icon"
                className="size-8 rounded-md"
                ariaLabel="Add contact or group"
              >
                {icon(icons.Plus, "size-4")}
              </Button>
            </span>
            <MenuItem onClick={() => (this.applyOpen = true)}>
              Add Contact / Group
            </MenuItem>
            <MenuItem onClick={() => (this.createGroupOpen = true)}>
              Create Group
            </MenuItem>
            <MenuItem onClick={() => this.showRequests()}>
              Friend Requests
            </MenuItem>
          </XMenu>
        </div>

        <div className="nice-scroll flex-1 overflow-y-auto">
          <CollapsibleSection
            title="Friends"
            count={this.friendList.length}
            open={this.friendsOpen}
            onToggle={() => (this.friendsOpen = !this.friendsOpen)}
          >
            {this.friendList.map((user) => (
              <div
                key={user.user_id}
                className="group hover:bg-accent/60 flex cursor-pointer items-center justify-between px-3 py-2 transition-colors duration-150"
              >
                <span
                  className="flex-1 truncate text-sm"
                  onClick={() => this.tryOpenChat(user.user_id)}
                >
                  {user.nickname}
                  {user.status === 1 && (
                    <span className="text-destructive ml-1 text-xs">
                      (blocked)
                    </span>
                  )}
                </span>
                {user.status === 1 && (
                  <Button
                    variant="ghost"
                    size="xs"
                    className="text-muted-foreground"
                    onClick={() => this.unblockUser(user.user_id)}
                  >
                    Unblock
                  </Button>
                )}
              </div>
            ))}
          </CollapsibleSection>

          <CollapsibleSection
            title="My Groups"
            count={this.myGroupList.length}
            open={this.myGroupsOpen}
            onToggle={() => {
              this.myGroupsOpen = !this.myGroupsOpen;
              if (this.myGroupsOpen) void this.loadMyGroups();
            }}
          >
            {this.myGroupList.map((group) => (
              <div
                key={group.group_id}
                className="hover:bg-accent/60 flex cursor-pointer items-center gap-2 px-3 py-2 transition-colors duration-150"
                onClick={() => this.tryOpenChat(group.group_id)}
              >
                {icon(icons.Users, "size-3.5 text-primary-deep")}
                <span className="truncate text-sm">{group.name}</span>
              </div>
            ))}
          </CollapsibleSection>

          <CollapsibleSection
            title="Joined Groups"
            count={this.joinedGroupList.length}
            open={this.joinedGroupsOpen}
            onToggle={() => {
              this.joinedGroupsOpen = !this.joinedGroupsOpen;
              if (this.joinedGroupsOpen) void this.loadJoinedGroups();
            }}
          >
            {this.joinedGroupList.map((group) => (
              <div
                key={group.group_id}
                className="hover:bg-accent/60 flex cursor-pointer items-center gap-2 px-3 py-2 transition-colors duration-150"
                onClick={() => this.tryOpenChat(group.group_id)}
              >
                {icon(icons.Users, "size-3.5 text-primary-deep")}
                <span className="truncate text-sm">{group.name}</span>
              </div>
            ))}
          </CollapsibleSection>
        </div>

        <x-dialog
          open={this.applyOpen}
          onClose={() => (this.applyOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Add Contact / Group</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="apply-id">User / Group ID</Label>
              <Input
                id="apply-id"
                placeholder="Enter ID"
                value={this.applyId}
                onValue={(v) => (this.applyId = v)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="apply-msg">Message</Label>
              <Textarea
                id="apply-msg"
                rows={2}
                placeholder="Optional"
                maxLength={100}
                value={this.applyMsg}
                onValue={(v) => (this.applyMsg = v)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button size="sm" onClick={() => this.submitApply()}>
              Submit
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (this.applyOpen = false)}
            >
              Cancel
            </Button>
          </DialogFooter>
        </x-dialog>

        <x-dialog
          open={this.createGroupOpen}
          onClose={() => (this.createGroupOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Create Group</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="group-name">Group Name</Label>
              <Input
                id="group-name"
                placeholder="Required"
                value={this.groupName}
                onValue={(v) => (this.groupName = v)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>Join Mode</Label>
              <RadioGroup
                name="create-addmode"
                value={String(this.groupAddMode)}
                options={[
                  { value: "0", label: "Direct Join" },
                  { value: "1", label: "Owner Approval" },
                ]}
                onValueChange={(v) => (this.groupAddMode = Number(v))}
              />
            </div>
          </div>
          <DialogFooter>
            <Button size="sm" onClick={() => this.submitCreateGroup()}>
              Create
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (this.createGroupOpen = false)}
            >
              Cancel
            </Button>
          </DialogFooter>
        </x-dialog>

        <x-dialog
          open={this.requestsOpen}
          onClose={() => (this.requestsOpen = false)}
        >
          <DialogHeader>
            <DialogTitle>Friend Requests</DialogTitle>
          </DialogHeader>
          {this.requestList.length === 0 ? (
            <p className="text-muted-foreground py-4 text-center text-sm">
              No pending requests
            </p>
          ) : (
            <div className="nice-scroll flex max-h-60 flex-col gap-2 overflow-y-auto">
              {this.requestList.map((req) => (
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
                      onClick={() => this.approveRequest(req.apply_id)}
                    >
                      Approve
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      className="text-muted-foreground"
                      onClick={() => this.refuseRequest(req.apply_id)}
                    >
                      Refuse
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      className="text-destructive"
                      onClick={() => this.blockRequest(req.apply_id)}
                    >
                      Block
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
          <DialogFooter>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => (this.requestsOpen = false)}
            >
              Close
            </Button>
          </DialogFooter>
        </x-dialog>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-contact-sidebar": XContactSidebar;
  }
}
