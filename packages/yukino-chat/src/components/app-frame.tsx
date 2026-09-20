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

import "@/components/nav-bar";
import { icon, icons } from "@/components/icons";

interface AppFrameProps {
  /** Path used to highlight the active rail item. */
  active: string;
  onLogout: () => void;
  sidebar?: unknown;
  children?: unknown;
}

/** Shared rounded app shell: nav rail + sidebar + content pane. */
export function AppFrame({
  active,
  onLogout,
  sidebar,
  children,
}: AppFrameProps) {
  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4 sm:p-6">
      <div className="border-border/70 bg-card shadow-primary/20 flex h-[min(760px,92vh)] w-full max-w-6xl overflow-hidden rounded-3xl border shadow-2xl">
        <x-nav-bar active={active} onLogout={onLogout} />
        {sidebar && (
          <aside className="border-border w-64 border-r">{sidebar}</aside>
        )}
        <main className="flex min-w-0 flex-1 flex-col">{children}</main>
      </div>
    </div>
  );
}

interface EmptyPaneProps {
  iconNode?: ReturnType<typeof icon>;
  hint: string;
}

export function EmptyPane({ iconNode, hint }: EmptyPaneProps) {
  return (
    <div className="text-muted-foreground/50 flex flex-1 flex-col items-center justify-center gap-3">
      <span className="bg-primary/15 flex size-16 items-center justify-center rounded-2xl">
        {iconNode ?? icon(icons.MessageSquare, "size-7")}
      </span>
      <p className="text-muted-foreground/70 text-sm">{hint}</p>
    </div>
  );
}
