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
