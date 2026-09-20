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
import { setLogin } from "@/store/auth";
import { connectWs } from "@/store/ws";
import { isValidPhone } from "@/utils/validate";
import { showToast } from "@/utils/toast";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { AuthShell } from "@/pages/auth-shell";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import type { AuthResponse } from "@/types";

@customElement("sc-register")
export class RegisterPage extends TwElement {
  @state() private nickname = "";
  @state() private telephone = "";
  @state() private password = "";
  @state() private confirmPassword = "";

  private async handleRegister() {
    const { nickname, telephone, password, confirmPassword } = this;
    if (!nickname || !telephone || !password || !confirmPassword) {
      showToast("Please fill in all fields", "error");
      return;
    }
    if (nickname.length < 3 || nickname.length > 10) {
      showToast("Nickname must be 3-10 characters", "error");
      return;
    }
    if (!isValidPhone(telephone)) {
      showToast("Invalid phone number", "error");
      return;
    }
    if (password !== confirmPassword) {
      showToast("Passwords do not match", "error");
      return;
    }
    const res = await api.register({ nickname, telephone, password });
    if (res.code === 200 && res.data) {
      const { token, user_info } = res.data as AuthResponse;
      showToast(res.message, "success");
      setLogin(token, user_info);
      connectWs(user_info.uuid);
      navigate("/chat/sessions");
    } else {
      showToast(res.message || "Registration failed", "error");
    }
  }

  override render() {
    return (
      <AuthShell
        title="Register"
        description="Create your Yukino Chat account"
        footer={
          <>
            <Button className="w-full" onClick={() => this.handleRegister()}>
              Register
            </Button>
            <div className="flex w-full justify-end">
              <a
                href="/login"
                className="text-primary-deep cursor-pointer text-sm hover:underline"
              >
                Sign In
              </a>
            </div>
          </>
        }
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="register-nickname">Nickname</Label>
          <Input
            id="register-nickname"
            placeholder="3-10 characters"
            value={this.nickname}
            onValue={(v) => (this.nickname = v)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="register-phone">Phone</Label>
          <Input
            id="register-phone"
            placeholder="Enter your phone number"
            value={this.telephone}
            onValue={(v) => (this.telephone = v)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="register-password">Password</Label>
          <Input
            id="register-password"
            type="password"
            placeholder="Enter your password"
            value={this.password}
            onValue={(v) => (this.password = v)}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="register-confirm-password">Confirm Password</Label>
          <Input
            id="register-confirm-password"
            type="password"
            placeholder="Re-enter your password"
            value={this.confirmPassword}
            onValue={(v) => (this.confirmPassword = v)}
          />
        </div>
      </AuthShell>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-register": RegisterPage;
  }
}
