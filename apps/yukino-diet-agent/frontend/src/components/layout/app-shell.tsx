import { useStore } from "@tanstack/react-store";
import { useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Gauge,
  Globe,
  LayoutDashboard,
  MessagesSquare,
  PanelLeftClose,
  PanelLeftOpen,
  Salad,
  UtensilsCrossed,
} from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router-dom";

import { ThemeToggle } from "@/components/layout/theme-toggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useUserId } from "@/hooks/use-user-id";
import { cn } from "@/lib/utils";
import { preferencesStore, toggleSidebar } from "@/stores/preferences";

const NAV_ITEMS = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard, end: true },
  { to: "/chat", label: "Chat", icon: MessagesSquare, end: false },
  {
    to: "/meals/personal",
    label: "Personal Meals",
    icon: UtensilsCrossed,
    end: false,
  },
  { to: "/meals/public", label: "Public Meals", icon: Globe, end: false },
  { to: "/traces", label: "Traces", icon: Activity, end: false },
  { to: "/evaluations", label: "Evaluations", icon: Gauge, end: false },
] as const;

export function AppShell() {
  const collapsed = useStore(preferencesStore).sidebarCollapsed;
  const location = useLocation();
  const queryClient = useQueryClient();
  const { userId, setUserId } = useUserId();
  const [draftUserId, setDraftUserId] = useState(userId);

  useEffect(() => {
    setDraftUserId(userId);
  }, [userId]);

  const commitUserId = () => {
    const next = draftUserId.trim() || "1";
    setDraftUserId(next);
    if (next !== userId) {
      setUserId(next);
      void queryClient.invalidateQueries();
    }
  };

  const activeItem = NAV_ITEMS.find((item) =>
    item.end
      ? location.pathname === item.to
      : location.pathname.startsWith(item.to),
  );

  return (
    <div className="bg-background text-foreground flex min-h-screen">
      <aside
        className={cn(
          "bg-sidebar sticky top-0 z-40 flex h-screen shrink-0 flex-col border-r transition-[width] duration-200",
          collapsed ? "w-16" : "w-60",
        )}
      >
        <div
          className={cn(
            "flex h-14 items-center gap-2 border-b px-3",
            collapsed && "justify-center px-0",
          )}
        >
          <span className="bg-primary text-primary-foreground flex size-8 shrink-0 items-center justify-center rounded-lg shadow-sm">
            <Salad className="size-4.5" />
          </span>
          {!collapsed && (
            <span className="flex min-w-0 flex-col leading-tight">
              <strong className="truncate text-sm">Diet Agent</strong>
              <small className="text-muted-foreground truncate text-xs">
                Eat smarter, lighter
              </small>
            </span>
          )}
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto p-2">
          {NAV_ITEMS.map((item) => {
            const isActive = item.end
              ? location.pathname === item.to
              : location.pathname.startsWith(item.to);
            return (
              <Tooltip key={item.to} disableHoverablePopup={collapsed}>
                <TooltipTrigger
                  render={<NavLink to={item.to} end={item.end} />}
                  className={cn(
                    "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                    collapsed && "justify-center px-0",
                  )}
                >
                  <item.icon className="size-4 shrink-0" />
                  {!collapsed && <span className="truncate">{item.label}</span>}
                  <span
                    className={cn(
                      "bg-primary ml-auto size-1.5 rounded-full transition-opacity",
                      isActive ? "opacity-100" : "opacity-0",
                    )}
                  />
                </TooltipTrigger>
                {collapsed && (
                  <TooltipContent side="right">{item.label}</TooltipContent>
                )}
              </Tooltip>
            );
          })}
        </nav>

        <div className={cn("border-t p-2", collapsed && "flex justify-center")}>
          <Button
            variant="ghost"
            size={collapsed ? "icon" : "sm"}
            onClick={toggleSidebar}
            className="text-muted-foreground w-full justify-start"
          >
            {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
            {!collapsed && <span className="ml-2">Collapse</span>}
          </Button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="bg-background/85 sticky top-0 z-30 flex h-14 items-center justify-between gap-3 border-b px-4 backdrop-blur md:px-6">
          <h1 className="truncate text-sm font-semibold tracking-tight">
            {activeItem?.label ?? "Diet Agent"}
          </h1>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <Label
                htmlFor="user-id"
                className="text-muted-foreground hidden text-xs sm:block"
              >
                User ID
              </Label>
              <Input
                id="user-id"
                value={draftUserId}
                onChange={(event) =>
                  setDraftUserId(event.target.value.replace(/[^0-9]/g, ""))
                }
                onBlur={commitUserId}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.currentTarget.blur();
                  }
                }}
                className="h-8 w-20 text-center"
                inputMode="numeric"
              />
            </div>
            <ThemeToggle />
          </div>
        </header>

        <main className="flex-1 p-4 md:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
