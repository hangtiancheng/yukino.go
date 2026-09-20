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

import { customElement, property } from "@yukino.js/lit-jsx";
import { authStore } from "@/store/auth";
import { TwElement } from "@/styles/base";
import "@/components/ui/avatar";
import { Tooltip } from "@/components/ui/menu";
import { icon, icons } from "@/components/icons";
import { cn } from "@/lib/utils";

const CHAT_EXEMPT_PATHS = ["/chat/contacts", "/chat/profile"];

interface RailItem {
  label: string;
  path: string;
  iconNode: ReturnType<typeof icon>;
  isActive: (active: string) => boolean;
}

const NAV_ITEMS: RailItem[] = [
  {
    label: "Sessions",
    path: "/chat/sessions",
    iconNode: icon(icons.MessageSquare, "size-5"),
    isActive: (active) =>
      active === "/chat/sessions" ||
      (active.startsWith("/chat/") && !CHAT_EXEMPT_PATHS.includes(active)),
  },
  {
    label: "Contacts",
    path: "/chat/contacts",
    iconNode: icon(icons.Users, "size-5"),
    isActive: (active) => active === "/chat/contacts",
  },
  {
    label: "Profile",
    path: "/chat/profile",
    iconNode: icon(icons.User, "size-5"),
    isActive: (active) => active === "/chat/profile",
  },
];

interface RailButtonProps {
  label: string;
  href?: string;
  onClick?: () => void;
  active?: boolean;
  iconNode: ReturnType<typeof icon>;
  destructive?: boolean;
}

function RailButton({
  label,
  href,
  onClick,
  active,
  iconNode,
  destructive,
}: RailButtonProps) {
  const cls = cn(
    "flex size-10 items-center justify-center rounded-xl transition-all duration-200 hover:scale-105 active:scale-95",
    active
      ? "bg-primary text-primary-foreground shadow-sm"
      : destructive
        ? "text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
        : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
  );
  return (
    <Tooltip label={label}>
      {onClick ? (
        <button
          type="button"
          aria-label={label}
          onClick={onClick}
          className={cls}
        >
          {iconNode}
        </button>
      ) : (
        <a href={href} aria-label={label} className={cls}>
          {iconNode}
        </a>
      )}
    </Tooltip>
  );
}

/**
 * Left navigation rail for the main app frame. Reads the auth store
 * reactively so the avatar updates after a profile change.
 */
@customElement("x-nav-bar")
export class XNavBar extends TwElement {
  @property() active = "";
  onLogout?: () => void;

  override render() {
    const auth = authStore.get();
    return (
      <nav className="border-border bg-primary/10 flex h-full w-16 flex-col items-center border-r py-4">
        <a href="/chat/profile" aria-label="Your profile">
          <x-avatar
            className="size-10"
            src={auth.user.avatar}
            name={auth.user.nickname || "U"}
          />
        </a>

        <div className="mt-6 flex flex-col items-center gap-1.5">
          {NAV_ITEMS.map((item) => (
            <RailButton
              key={item.path}
              label={item.label}
              href={item.path}
              active={item.isActive(this.active)}
              iconNode={item.iconNode}
            />
          ))}
        </div>

        <div className="flex-1"></div>

        <div className="flex flex-col items-center gap-1.5">
          <div className="bg-border mb-1 h-px w-8" aria-hidden="true"></div>
          {auth.user.is_admin === 1 && (
            <RailButton
              label="Admin"
              href="/manager"
              active={this.active === "/manager"}
              iconNode={icon(icons.Settings, "size-5")}
            />
          )}
          <RailButton
            label="Sign Out"
            onClick={() => this.onLogout?.()}
            destructive
            iconNode={icon(icons.LogOut, "size-5")}
          />
        </div>
      </nav>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-nav-bar": XNavBar;
  }
}
