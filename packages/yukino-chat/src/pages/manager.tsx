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
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/collapsible";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { MenuLabel } from "@/components/ui/menu";
import { icon, icons } from "@/components/icons";
import { cn } from "@/lib/utils";

type Panel =
  | "none"
  | "disable-user"
  | "delete-user"
  | "set-admin"
  | "disable-group"
  | "delete-group";

interface UserRow {
  uuid: string;
  nickname: string;
  telephone: string;
  is_admin: number;
  status: number;
  is_deleted?: boolean;
}

interface GroupRow {
  group_id: string;
  name: string;
  owner_id: string;
  member_cnt: number;
  avatar: string;
  status: number;
  is_deleted?: boolean;
}

const USER_PANELS: { panel: Panel; label: string }[] = [
  { panel: "disable-user", label: "Enable / Disable" },
  { panel: "delete-user", label: "Delete" },
  { panel: "set-admin", label: "Set as Admin" },
];

const GROUP_PANELS: { panel: Panel; label: string }[] = [
  { panel: "disable-group", label: "Enable / Disable" },
  { panel: "delete-group", label: "Delete / Disband" },
];

function SideMenuItem({
  active,
  label,
  onSelect,
}: {
  active: boolean;
  label: string;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-current={active ? "page" : undefined}
      onClick={onSelect}
      className={cn(
        "hover:bg-accent w-full cursor-pointer rounded-lg px-3 py-2 text-left text-sm font-normal transition-colors",
        active
          ? "bg-primary/40 text-foreground font-medium"
          : "text-muted-foreground",
      )}
    >
      {label}
    </button>
  );
}

@customElement("sc-manager")
export class ManagerPage extends TwElement {
  @state() private currentPanel: Panel = "none";
  @state() private userList: UserRow[] = [];
  @state() private groupList: GroupRow[] = [];
  @state() private selectedUserIds: string[] = [];
  @state() private selectedGroupIds: string[] = [];

  private async loadUserList() {
    const uid = currentUser().uuid;
    const res = await api.getUserInfoList({ owner_id: uid });
    if (res.code !== 200) {
      showToast(res.message || "Failed to load users", "error");
      return;
    }
    this.userList = (res.data as UserRow[]) || [];
    this.selectedUserIds = [];
  }

  private async loadGroupList() {
    const res = await api.getGroupInfoList({});
    if (res.code !== 200) {
      showToast(res.message || "Failed to load groups", "error");
      return;
    }
    this.groupList = (res.data as GroupRow[]) || [];
    this.selectedGroupIds = [];
  }

  private showPanel(panel: Panel) {
    this.currentPanel = panel;
    this.selectedUserIds = [];
    this.selectedGroupIds = [];
    if (
      panel === "disable-user" ||
      panel === "delete-user" ||
      panel === "set-admin"
    ) {
      void this.loadUserList();
    } else if (panel === "disable-group" || panel === "delete-group") {
      void this.loadGroupList();
    }
  }

  private toggleUser(uuid: string, checked: boolean) {
    this.selectedUserIds = checked
      ? [...this.selectedUserIds, uuid]
      : this.selectedUserIds.filter((id) => id !== uuid);
  }

  private toggleAllUsers(checked: boolean) {
    this.selectedUserIds = checked ? this.userList.map((u) => u.uuid) : [];
  }

  private toggleGroup(uuid: string, checked: boolean) {
    this.selectedGroupIds = checked
      ? [...this.selectedGroupIds, uuid]
      : this.selectedGroupIds.filter((id) => id !== uuid);
  }

  private toggleAllGroups(checked: boolean) {
    this.selectedGroupIds = checked
      ? this.groupList.map((g) => g.group_id)
      : [];
  }

  private requireSelection(ids: string[], msg: string): boolean {
    if (ids.length === 0) {
      showToast(msg, "warning");
      return false;
    }
    return true;
  }

  private async runUserAction(
    action: () => Promise<{ code: number; message: string }>,
    successMsg: string,
  ) {
    if (!this.requireSelection(this.selectedUserIds, "No users selected"))
      return;
    const res = await action();
    if (res.code === 200) {
      showToast(successMsg, "success");
      void this.loadUserList();
    } else {
      showToast(res.message || "Operation failed", "error");
    }
  }

  private async runGroupAction(
    action: () => Promise<{ code: number; message: string }>,
    successMsg: string,
  ) {
    if (!this.requireSelection(this.selectedGroupIds, "No groups selected"))
      return;
    const res = await action();
    if (res.code === 200) {
      showToast(successMsg, "success");
      void this.loadGroupList();
    } else {
      showToast(res.message || "Operation failed", "error");
    }
  }

  private get isUserPanel() {
    return (
      this.currentPanel === "disable-user" ||
      this.currentPanel === "delete-user" ||
      this.currentPanel === "set-admin"
    );
  }

  private get isGroupPanel() {
    return (
      this.currentPanel === "disable-group" ||
      this.currentPanel === "delete-group"
    );
  }

  private get allUsersChecked() {
    return (
      this.userList.length > 0 &&
      this.selectedUserIds.length === this.userList.length
    );
  }

  private get allGroupsChecked() {
    return (
      this.groupList.length > 0 &&
      this.selectedGroupIds.length === this.groupList.length
    );
  }

  override render() {
    return (
      <div className="bg-background flex min-h-screen items-center justify-center p-4 sm:p-6">
        <div className="border-border/70 bg-card shadow-primary/20 flex h-[min(760px,92vh)] w-full max-w-6xl flex-col overflow-hidden rounded-3xl border shadow-2xl">
          <div className="border-border bg-primary/10 flex h-14 shrink-0 items-center justify-between border-b px-6">
            <div className="flex items-center gap-3">
              {icon(icons.Shield, "size-5 text-primary-deep")}
              <span className="text-foreground text-lg font-semibold">
                Admin Panel
              </span>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate("/chat/sessions")}
            >
              Back
            </Button>
          </div>

          <div className="flex flex-1 overflow-hidden">
            <div className="border-border bg-primary/5 nice-scroll w-52 shrink-0 overflow-y-auto border-r p-2">
              <MenuLabel>Users</MenuLabel>
              <div className="flex flex-col gap-0.5">
                {USER_PANELS.map(({ panel, label }) => (
                  <SideMenuItem
                    key={panel}
                    active={this.currentPanel === panel}
                    label={label}
                    onSelect={() => this.showPanel(panel)}
                  />
                ))}
              </div>
              <MenuLabel>Groups</MenuLabel>
              <div className="flex flex-col gap-0.5">
                {GROUP_PANELS.map(({ panel, label }) => (
                  <SideMenuItem
                    key={panel}
                    active={this.currentPanel === panel}
                    label={label}
                    onSelect={() => this.showPanel(panel)}
                  />
                ))}
              </div>
            </div>

            <div className="nice-scroll flex-1 overflow-y-auto">
              {this.currentPanel === "none" && (
                <div className="flex h-full items-center justify-center">
                  <p className="text-muted-foreground text-sm">
                    Select an option from the left menu
                  </p>
                </div>
              )}

              {this.isUserPanel && this.renderUserPanel()}
              {this.isGroupPanel && this.renderGroupPanel()}
            </div>
          </div>
        </div>
      </div>
    );
  }

  private renderUserPanel() {
    return (
      <div className="p-4">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-10">
                <Checkbox
                  checked={this.allUsersChecked}
                  ariaLabel="Select all users"
                  onCheckedChange={(checked) => this.toggleAllUsers(checked)}
                />
              </TableHead>
              <TableHead>UUID</TableHead>
              <TableHead>Nickname</TableHead>
              <TableHead>Phone</TableHead>
              <TableHead>Admin</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {this.userList.map((user) => (
              <TableRow key={user.uuid}>
                <TableCell>
                  <Checkbox
                    checked={this.selectedUserIds.includes(user.uuid)}
                    ariaLabel={`Select user ${user.nickname}`}
                    onCheckedChange={(checked) =>
                      this.toggleUser(user.uuid, checked)
                    }
                  />
                </TableCell>
                <TableCell className="text-muted-foreground font-mono text-xs">
                  {user.uuid}
                </TableCell>
                <TableCell>{user.nickname}</TableCell>
                <TableCell>{user.telephone}</TableCell>
                <TableCell>
                  {user.is_admin === 1 ? (
                    <Badge variant="success">Yes</Badge>
                  ) : (
                    <Badge variant="outline">No</Badge>
                  )}
                </TableCell>
                <TableCell>
                  {user.status === 1 ? (
                    <Badge variant="destructive">Disabled</Badge>
                  ) : (
                    <Badge variant="success">Active</Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <div className="mt-4 flex justify-end gap-2">
          {this.currentPanel === "disable-user" && (
            <>
              <Button
                onClick={() =>
                  this.runUserAction(
                    () => api.ableUsers({ uuid_list: this.selectedUserIds }),
                    "Users enabled",
                  )
                }
              >
                Enable
              </Button>
              <Button
                variant="destructive"
                onClick={() =>
                  this.runUserAction(
                    () => api.disableUsers({ uuid_list: this.selectedUserIds }),
                    "Users disabled",
                  )
                }
              >
                Disable
              </Button>
            </>
          )}
          {this.currentPanel === "delete-user" && (
            <Button
              variant="destructive"
              onClick={() =>
                this.runUserAction(
                  () => api.deleteUsers({ uuid_list: this.selectedUserIds }),
                  "Users deleted",
                )
              }
            >
              Delete
            </Button>
          )}
          {this.currentPanel === "set-admin" && (
            <>
              <Button
                onClick={() =>
                  this.runUserAction(
                    () =>
                      api.setAdmin({
                        uuid_list: this.selectedUserIds,
                        is_admin: 1,
                      }),
                    "Admin granted",
                  )
                }
              >
                Grant Admin
              </Button>
              <Button
                variant="ghost"
                onClick={() =>
                  this.runUserAction(
                    () =>
                      api.setAdmin({
                        uuid_list: this.selectedUserIds,
                        is_admin: 0,
                      }),
                    "Admin revoked",
                  )
                }
              >
                Revoke Admin
              </Button>
            </>
          )}
        </div>
      </div>
    );
  }

  private renderGroupPanel() {
    return (
      <div className="p-4">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-10">
                <Checkbox
                  checked={this.allGroupsChecked}
                  ariaLabel="Select all groups"
                  onCheckedChange={(checked) => this.toggleAllGroups(checked)}
                />
              </TableHead>
              <TableHead>Group ID</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Owner</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {this.groupList.map((group) => (
              <TableRow key={group.group_id}>
                <TableCell>
                  <Checkbox
                    checked={this.selectedGroupIds.includes(group.group_id)}
                    ariaLabel={`Select group ${group.name}`}
                    onCheckedChange={(checked) =>
                      this.toggleGroup(group.group_id, checked)
                    }
                  />
                </TableCell>
                <TableCell className="text-muted-foreground font-mono text-xs">
                  {group.group_id}
                </TableCell>
                <TableCell>{group.name}</TableCell>
                <TableCell className="text-muted-foreground font-mono text-xs">
                  {group.owner_id}
                </TableCell>
                <TableCell>
                  {group.is_deleted ? (
                    <Badge variant="outline">Deleted</Badge>
                  ) : group.status === 1 ? (
                    <Badge variant="destructive">Disabled</Badge>
                  ) : (
                    <Badge variant="success">Active</Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <div className="mt-4 flex justify-end gap-2">
          {this.currentPanel === "disable-group" && (
            <>
              <Button
                onClick={() =>
                  this.runGroupAction(
                    () =>
                      api.setGroupsStatus({
                        uuid_list: this.selectedGroupIds,
                        status: 0,
                      }),
                    "Groups enabled",
                  )
                }
              >
                Enable
              </Button>
              <Button
                variant="destructive"
                onClick={() =>
                  this.runGroupAction(
                    () =>
                      api.setGroupsStatus({
                        uuid_list: this.selectedGroupIds,
                        status: 1,
                      }),
                    "Groups disabled",
                  )
                }
              >
                Disable
              </Button>
            </>
          )}
          {this.currentPanel === "delete-group" && (
            <Button
              variant="destructive"
              onClick={() =>
                this.runGroupAction(
                  () => api.deleteGroups({ uuid_list: this.selectedGroupIds }),
                  "Groups deleted",
                )
              }
            >
              Delete
            </Button>
          )}
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-manager": ManagerPage;
  }
}
