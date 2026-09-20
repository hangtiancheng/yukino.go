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
import { authStore, currentUser, updateUserInfo } from "@/store/auth";
import { showToast } from "@/utils/toast";
import { isValidEmail } from "@/utils/validate";
import { performLogout } from "@/utils/logout";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { AppFrame } from "@/components/app-frame";
import "@/components/contact-sidebar";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import {
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { icon, icons } from "@/components/icons";
import type { UserInfo } from "@/types";

@customElement("sc-profile")
export class OwnInfoPage extends TwElement {
  @state() private editOpen = false;
  @state() private editNick = "";
  @state() private editEmail = "";
  @state() private editBirthday = "";
  @state() private editSig = "";
  @state() private avatarFile: File | null = null;

  private closeEditModal() {
    this.editOpen = false;
    this.editNick = "";
    this.editEmail = "";
    this.editBirthday = "";
    this.editSig = "";
    this.avatarFile = null;
  }

  private async saveProfile() {
    const user = currentUser();
    if (
      !this.editNick &&
      !this.editEmail &&
      !this.editBirthday &&
      !this.editSig &&
      !this.avatarFile
    ) {
      showToast("Please modify at least one field", "warning");
      return;
    }
    if (
      this.editNick &&
      (this.editNick.length < 3 || this.editNick.length > 10)
    ) {
      showToast("Nickname must be 3-10 characters", "error");
      return;
    }
    if (this.editEmail && !isValidEmail(this.editEmail)) {
      showToast("Invalid email address", "error");
      return;
    }
    const data: Record<string, unknown> = { uuid: user.uuid };
    if (this.editNick) data.nickname = this.editNick;
    if (this.editEmail) data.email = this.editEmail;
    if (this.editBirthday) data.birthday = this.editBirthday;
    if (this.editSig) data.signature = this.editSig;
    let avatarUrl = "";
    if (this.avatarFile) {
      const formData = new FormData();
      formData.append("file", this.avatarFile);
      const uploadRes = await api.uploadAvatar(formData);
      avatarUrl = (uploadRes.data as { url?: string } | null)?.url ?? "";
      if (uploadRes.code !== 200 || !avatarUrl) {
        showToast(uploadRes.message || "Avatar upload failed", "error");
        return;
      }
      data.avatar = avatarUrl;
    }
    const res = await api.updateUserInfo(data);
    if (res.code === 200) {
      showToast(res.message, "success");
      const updated: UserInfo = { ...user };
      if (this.editNick) updated.nickname = this.editNick;
      if (this.editEmail) updated.email = this.editEmail;
      if (this.editBirthday) updated.birthday = this.editBirthday;
      if (this.editSig) updated.signature = this.editSig;
      if (avatarUrl) updated.avatar = avatarUrl;
      updateUserInfo(updated);
      this.closeEditModal();
    } else {
      showToast(res.message, "error");
    }
  }

  override render() {
    const user = authStore.get().user;
    return (
      <AppFrame
        active="/chat/profile"
        onLogout={async () => {
          await performLogout();
          navigate("/login");
        }}
        sidebar={
          <x-contact-sidebar
            onNavigate={(id: string) => navigate(`/chat/${id}`)}
          />
        }
      >
        <div className="nice-scroll relative flex flex-1 flex-col items-center justify-center overflow-y-auto p-8">
          <div className="flex flex-col items-center gap-3">
            <x-avatar
              className="ring-primary/30 size-20 text-lg ring-4"
              src={user.avatar}
              name={user.nickname}
            />
            <div className="text-center">
              <h2 className="text-xl font-semibold">{user.nickname}</h2>
              <p className="text-muted-foreground mt-0.5 text-sm">
                {user.signature || "No signature yet"}
              </p>
            </div>
          </div>

          <div className="text-foreground mt-8 flex w-full max-w-sm flex-col gap-2 text-sm">
            <InfoLine label="User ID" value={user.uuid} mono />
            <InfoLine label="Phone" value={user.telephone} />
            <InfoLine label="Email" value={user.email} />
            <InfoLine
              label="Gender"
              value={user.gender === 0 ? "Male" : "Female"}
            />
            <InfoLine label="Birthday" value={user.birthday} />
            <InfoLine label="Joined" value={user.created_at} />
          </div>

          <Button
            size="sm"
            className="absolute right-6 bottom-6"
            onClick={() => (this.editOpen = true)}
          >
            {icon(icons.User, "size-4")}
            Edit
          </Button>

          <x-dialog open={this.editOpen} onClose={() => this.closeEditModal()}>
            <DialogHeader>
              <DialogTitle>Edit Profile</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="edit-nickname">Nickname</Label>
                <Input
                  id="edit-nickname"
                  placeholder="Optional, 3-10 characters"
                  value={this.editNick}
                  onValue={(v) => (this.editNick = v)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="edit-email">Email</Label>
                <Input
                  id="edit-email"
                  placeholder="Optional"
                  value={this.editEmail}
                  onValue={(v) => (this.editEmail = v)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="edit-birthday">Birthday</Label>
                <Input
                  id="edit-birthday"
                  placeholder="Optional, e.g. 2024.1.1"
                  value={this.editBirthday}
                  onValue={(v) => (this.editBirthday = v)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="edit-signature">Signature</Label>
                <Input
                  id="edit-signature"
                  placeholder="Optional"
                  value={this.editSig}
                  onValue={(v) => (this.editSig = v)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="edit-avatar">Avatar</Label>
                <Input
                  id="edit-avatar"
                  type="file"
                  accept="image/*"
                  onChange={(e: Event) => {
                    const input = e.target as HTMLInputElement;
                    this.avatarFile = input.files?.[0] ?? null;
                  }}
                />
              </div>
            </div>
            <DialogFooter>
              <Button size="sm" onClick={() => this.saveProfile()}>
                Save
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => this.closeEditModal()}
              >
                Cancel
              </Button>
            </DialogFooter>
          </x-dialog>
        </div>
      </AppFrame>
    );
  }
}

function InfoLine({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="border-border flex items-baseline justify-between gap-4 border-b py-1.5">
      <span className="text-muted-foreground shrink-0">{label}</span>
      <span
        className={mono ? "truncate font-mono text-xs" : "truncate font-medium"}
      >
        {value}
      </span>
    </div>
  );
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-profile": OwnInfoPage;
  }
}
