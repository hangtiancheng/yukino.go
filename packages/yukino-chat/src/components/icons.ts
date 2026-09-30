import { createElement, type IconNode } from "lucide";

/**
 * Lit stand-in for lucide-react components: returns a styled inline SVG
 * element that can be interpolated into lit-html templates.
 */
export function icon(node: IconNode, classes = "size-4"): SVGElement {
  const el = createElement(node);
  el.setAttribute("class", classes);
  return el;
}

import {
  ChartBar,
  CheckCheck,
  ChevronDown,
  CircleAlert,
  CircleCheck,
  Download,
  EllipsisVertical,
  FileText,
  Info,
  LogOut,
  MessageCircle,
  MessageSquare,
  Paperclip,
  Plus,
  Search,
  Send,
  Settings,
  Shield,
  TriangleAlert,
  User,
  Users,
  Video,
  X,
} from "lucide";

export const icons = {
  MessageSquare,
  MessageCircle,
  Users,
  User,
  Settings,
  LogOut,
  Paperclip,
  Video,
  Shield,
  ChartBar,
  ChevronDown,
  Plus,
  EllipsisVertical,
  Download,
  FileText,
  CheckCheck,
  Search,
  Send,
  X,
  CircleCheck,
  CircleAlert,
  TriangleAlert,
  Info,
};
