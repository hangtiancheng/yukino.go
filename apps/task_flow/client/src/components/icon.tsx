import { unsafeHTML } from "lit/directives/unsafe-html.js";

import activity from "lucide-static/icons/activity.svg?raw";
import alertTriangle from "lucide-static/icons/alert-triangle.svg?raw";
import ban from "lucide-static/icons/ban.svg?raw";
import calendarClock from "lucide-static/icons/calendar-clock.svg?raw";
import checkCircle from "lucide-static/icons/check-circle.svg?raw";
import chevronLeft from "lucide-static/icons/chevron-left.svg?raw";
import chevronRight from "lucide-static/icons/chevron-right.svg?raw";
import clock from "lucide-static/icons/clock.svg?raw";
import database from "lucide-static/icons/database.svg?raw";
import externalLink from "lucide-static/icons/external-link.svg?raw";
import eye from "lucide-static/icons/eye.svg?raw";
import fileText from "lucide-static/icons/file-text.svg?raw";
import flaskConical from "lucide-static/icons/flask-conical.svg?raw";
import history from "lucide-static/icons/history.svg?raw";
import hourglass from "lucide-static/icons/hourglass.svg?raw";
import layoutDashboard from "lucide-static/icons/layout-dashboard.svg?raw";
import listChecks from "lucide-static/icons/list-checks.svg?raw";
import loaderCircle from "lucide-static/icons/loader-circle.svg?raw";
import mailWarning from "lucide-static/icons/mail-warning.svg?raw";
import play from "lucide-static/icons/play.svg?raw";
import plus from "lucide-static/icons/plus.svg?raw";
import power from "lucide-static/icons/power.svg?raw";
import refreshCw from "lucide-static/icons/refresh-cw.svg?raw";
import scrollText from "lucide-static/icons/scroll-text.svg?raw";
import server from "lucide-static/icons/server.svg?raw";
import shieldAlert from "lucide-static/icons/shield-alert.svg?raw";
import siren from "lucide-static/icons/siren.svg?raw";
import terminal from "lucide-static/icons/terminal.svg?raw";
import trash2 from "lucide-static/icons/trash-2.svg?raw";
import xCircle from "lucide-static/icons/x-circle.svg?raw";
import zap from "lucide-static/icons/zap.svg?raw";

const icons = {
  activity,
  "alert-triangle": alertTriangle,
  ban,
  "calendar-clock": calendarClock,
  "check-circle": checkCircle,
  "chevron-left": chevronLeft,
  "chevron-right": chevronRight,
  clock,
  database,
  "external-link": externalLink,
  eye,
  "file-text": fileText,
  "flask-conical": flaskConical,
  history,
  hourglass,
  "layout-dashboard": layoutDashboard,
  "list-checks": listChecks,
  "loader-circle": loaderCircle,
  "mail-warning": mailWarning,
  play,
  plus,
  power,
  "refresh-cw": refreshCw,
  "scroll-text": scrollText,
  server,
  "shield-alert": shieldAlert,
  siren,
  terminal,
  "trash-2": trash2,
  "x-circle": xCircle,
  zap,
} as const;

export type IconName = keyof typeof icons;

export function Icon(props: { name: IconName; class?: string }) {
  return (
    <span
      aria-hidden="true"
      class={`inline-flex shrink-0 [&>svg]:h-full [&>svg]:w-full ${props.class ?? "h-4 w-4"}`}
    >
      {unsafeHTML(icons[props.name])}
    </span>
  );
}
