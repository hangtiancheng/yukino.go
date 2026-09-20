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

import { customElement, query, state } from "@yukino.js/lit-jsx";
import {
  connectDashboard,
  disconnectDashboard,
  deleteDashboardKey,
  dashboardStore,
} from "@/store/dashboard";
import { WS_URL } from "@/config";
import { formatSize, formatExpire } from "@/utils/format";
import { TwElement } from "@/styles/base";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/card";
import { icon, icons } from "@/components/icons";

const ROW_HEIGHT = 36;
const OVER_SCAN = 5;
const DASHBOARD_WS = WS_URL + "/dashboard/ws";

interface FlatRow {
  group: string;
  key: string;
  size: number;
  sizeStr: string;
  level: number;
  expire_at: number;
  expireStr: string;
}

function flatten(
  groups: {
    name: string;
    entries?: { key: string; size: number; level: number; expire_at: number }[];
  }[],
): FlatRow[] {
  const rows: FlatRow[] = [];
  for (const g of groups) {
    if (!g.entries) continue;
    for (const e of g.entries) {
      rows.push({
        group: g.name,
        key: e.key,
        size: e.size,
        sizeStr: formatSize(e.size),
        level: e.level,
        expire_at: e.expire_at,
        expireStr: formatExpire(e.expire_at),
      });
    }
  }
  return rows;
}

@customElement("sc-dashboard")
export class DashboardPage extends TwElement {
  @state() private totalEntries = 0;
  @state() private status = "disconnected";
  @state() private visibleRows: FlatRow[] = [];
  @state() private topPad = 0;
  @state() private bottomPad = 0;

  private allRows: FlatRow[] = [];
  private pollTimer: number | null = null;
  private raf = 0;
  private scrollTopPos = 0;

  @query("#dash-scroll") private scrollEl!: HTMLDivElement;

  override connectedCallback() {
    super.connectedCallback();
    connectDashboard(DASHBOARD_WS);
    this.pollTimer = window.setInterval(() => {
      this.syncView();
    }, 500);
    this.syncView();
  }

  override disconnectedCallback() {
    if (this.pollTimer !== null) clearInterval(this.pollTimer);
    cancelAnimationFrame(this.raf);
    disconnectDashboard();
    super.disconnectedCallback();
  }

  private syncView() {
    const s = dashboardStore.get();
    this.allRows = flatten(s.groups);
    const el = this.scrollEl;
    const containerHeight = el?.clientHeight ?? 0;
    const totalRows = this.allRows.length;
    const visibleCount =
      Math.ceil(containerHeight / ROW_HEIGHT) + OVER_SCAN * 2;
    const startIdx = Math.max(
      0,
      Math.floor(this.scrollTopPos / ROW_HEIGHT) - OVER_SCAN,
    );
    const endIdx = Math.min(totalRows, startIdx + visibleCount);
    this.totalEntries = totalRows;
    this.visibleRows = this.allRows.slice(startIdx, endIdx);
    this.topPad = startIdx * ROW_HEIGHT;
    this.bottomPad = Math.max(0, (totalRows - endIdx) * ROW_HEIGHT);
    this.status = s.status;
  }

  private onScroll = () => {
    const el = this.scrollEl;
    if (!el) return;
    this.scrollTopPos = el.scrollTop;
    cancelAnimationFrame(this.raf);
    this.raf = requestAnimationFrame(() => this.syncView());
  };

  override render() {
    const status = this.status;
    return (
      <div className="bg-background flex min-h-screen items-center justify-center p-4 sm:p-6">
        <div className="border-border/70 bg-card shadow-primary/20 flex h-[min(820px,94vh)] w-full max-w-6xl flex-col overflow-hidden rounded-3xl border shadow-2xl">
          <div className="border-border bg-primary/10 flex h-14 shrink-0 items-center justify-between border-b px-6">
            <div className="flex items-center gap-3">
              {icon(icons.ChartBar, "size-5 text-primary-deep")}
              <span className="text-foreground text-lg font-semibold">
                Cache Dashboard
              </span>
            </div>
            <div className="flex items-center gap-3">
              <StatusBadge status={status} />
              <span className="text-muted-foreground text-xs">
                {this.totalEntries} entries
              </span>
            </div>
          </div>

          {status !== "connected" && (
            <div className="flex flex-1 items-center justify-center">
              <div className="text-center">
                <p className="text-muted-foreground mb-4 text-sm">
                  WebSocket not connected
                </p>
                <Button
                  size="sm"
                  onClick={() => connectDashboard(DASHBOARD_WS)}
                >
                  Connect
                </Button>
              </div>
            </div>
          )}

          {status === "connected" && (
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="border-border bg-primary/5 text-foreground flex h-10 shrink-0 items-center gap-2 border-b px-4 text-xs font-medium">
                <div className="w-16 text-center">Group</div>
                <div className="flex-1">Key</div>
                <div className="w-16 text-right">Size</div>
                <div className="w-14 text-center">Level</div>
                <div className="w-40 text-center">Expires</div>
                <div className="w-16 text-center">Action</div>
              </div>

              <div
                id="dash-scroll"
                className="nice-scroll flex-1 overflow-y-auto"
                onScroll={this.onScroll}
              >
                <div
                  style={{
                    paddingTop: `${this.topPad}px`,
                    paddingBottom: `${this.bottomPad}px`,
                  }}
                >
                  {this.visibleRows.map((row) => (
                    <div
                      key={row.group + row.key}
                      className="border-border hover:bg-accent/40 flex h-9 items-center gap-2 border-b px-4 text-sm transition-colors"
                    >
                      <div className="w-16 text-center">
                        <Badge variant="outline" className="font-normal">
                          {row.group}
                        </Badge>
                      </div>
                      <div
                        className="flex-1 truncate font-mono text-xs"
                        title={row.key}
                      >
                        {row.key}
                      </div>
                      <div className="text-muted-foreground w-16 text-right text-xs">
                        {row.sizeStr}
                      </div>
                      <div className="w-14 text-center">
                        {row.level === 1 ? (
                          <Badge variant="success">hot</Badge>
                        ) : (
                          <Badge variant="secondary">cold</Badge>
                        )}
                      </div>
                      <div className="text-muted-foreground w-40 text-center text-xs">
                        {row.expireStr}
                      </div>
                      <div className="w-16 text-center">
                        <Button
                          variant="outline"
                          size="xs"
                          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                          onClick={() => deleteDashboardKey(row.group, row.key)}
                        >
                          Delete
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }
}

function StatusBadge({ status }: { status: string }) {
  if (status === "connected") return <Badge variant="success">Connected</Badge>;
  if (status === "connecting")
    return <Badge variant="warning">Connecting</Badge>;
  return <Badge variant="destructive">Disconnected</Badge>;
}

declare global {
  interface HTMLElementTagNameMap {
    "sc-dashboard": DashboardPage;
  }
}
