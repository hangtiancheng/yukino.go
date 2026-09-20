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
import "@/components/contact-sidebar";
import { icon, icons } from "@/components/icons";

@customElement("sc-contact-list")
export class ContactListPage extends TwElement {
  override render() {
    return (
      <AppFrame
        active="/chat/contacts"
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
        <EmptyPane
          iconNode={icon(icons.User, "size-7")}
          hint="Select a contact to start chatting"
        />
      </AppFrame>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-contact-list": ContactListPage;
  }
}
