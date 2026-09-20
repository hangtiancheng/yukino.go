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

import { customElement } from "@yukino.js/lit-jsx";
import { performLogout } from "@/utils/logout";
import { navigate } from "@/router";
import { TwElement } from "@/styles/base";
import { AppFrame, EmptyPane } from "@/components/app-frame";
import "@/components/session-sidebar";
import { icon, icons } from "@/components/icons";

@customElement("sc-session-list")
export class SessionListPage extends TwElement {
  override render() {
    return (
      <AppFrame
        active="/chat/sessions"
        onLogout={async () => {
          await performLogout();
          navigate("/login");
        }}
        sidebar={
          <x-session-sidebar onChat={(id: string) => navigate(`/chat/${id}`)} />
        }
      >
        <EmptyPane
          iconNode={icon(icons.MessageSquare, "size-7")}
          hint="Select a conversation to start chatting"
        />
      </AppFrame>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-session-list": SessionListPage;
  }
}
