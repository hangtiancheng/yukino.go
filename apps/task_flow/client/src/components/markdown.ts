function escapeHtml(input: string): string {
  return input
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function inline(escaped: string): string {
  return escaped
    .replace(
      /`([^`]+)`/g,
      '<code class="rounded bg-base-200 px-1 py-0.5 text-[0.85em] text-primary">$1</code>',
    )
    .replace(
      /\*\*([^*]+)\*\*/g,
      '<strong class="font-semibold text-base-content">$1</strong>',
    )
    .replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>")
    .replace(
      /\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g,
      '<a class="link link-primary" href="$2" target="_blank" rel="noopener noreferrer">$1</a>',
    );
}

interface FrontMatter {
  entries: Array<[string, string]>;
  rest: string;
}

export function splitFrontMatter(doc: string): FrontMatter | null {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(doc);
  if (!match) return null;
  const entries: Array<[string, string]> = [];
  for (const line of match[1].split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx <= 0) continue;
    entries.push([line.slice(0, idx).trim(), line.slice(idx + 1).trim()]);
  }
  return { entries, rest: doc.slice(match[0].length) };
}

const headCls: Record<string, string> = {
  h1: "mt-6 mb-3 text-2xl font-semibold tracking-tight text-base-content first:mt-0",
  h2: "mt-6 mb-3 border-b border-base-300 pb-2 text-xl font-semibold text-base-content first:mt-0",
  h3: "mt-5 mb-2 text-lg font-semibold text-base-content",
  h4: "mt-4 mb-2 text-base font-semibold text-base-content",
};

function renderTable(lines: string[]): string {
  const rows = lines
    .map((l) =>
      l
        .replace(/^\||\|$/g, "")
        .split("|")
        .map((c) => c.trim()),
    )
    .filter((cells) => cells.length > 0);
  if (rows.length === 0) return "";

  const isSeparator = (cells: string[]) =>
    cells.every((c) => /^:?-{3,}:?$/.test(c) || c === "");
  let head: string[] = [];
  let body: string[][] = [];
  if (rows.length >= 2 && isSeparator(rows[1])) {
    head = rows[0];
    body = rows.slice(2);
  } else {
    body = rows;
  }

  const th = (c: string) =>
    `<th class="bg-base-200/70 px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-base-content/70">${inline(escapeHtml(c))}</th>`;
  const td = (c: string) =>
    `<td class="px-3 py-2 align-top text-sm text-base-content/90">${inline(escapeHtml(c))}</td>`;

  let html =
    '<div class="my-3 overflow-x-auto rounded-lg border border-base-300"><table class="table table-sm">';
  if (head.length > 0) {
    html += `<thead><tr>${head.map(th).join("")}</tr></thead>`;
  }
  html += `<tbody>${body.map((r) => `<tr class="border-t border-base-300">${r.map(td).join("")}</tr>`).join("")}</tbody>`;
  html += "</table></div>";
  return html;
}

export function renderMarkdown(doc: string): string {
  const out: string[] = [];
  const lines = doc.split(/\r?\n/);

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    if (/^```/.test(line.trim())) {
      const lang = line.trim().slice(3).trim();
      const buf: string[] = [];
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i].trim())) {
        buf.push(lines[i]);
        i += 1;
      }
      i += 1;
      out.push(
        `<div class="my-3"><div class="flex items-center justify-between rounded-t-lg bg-base-300/60 px-3 py-1.5 text-xs text-base-content/60"><span>${escapeHtml(lang || "code")}</span></div><pre class="overflow-x-auto rounded-b-lg bg-base-200 p-3 text-[13px] leading-relaxed"><code>${escapeHtml(buf.join("\n"))}</code></pre></div>`,
      );
      continue;
    }

    if (/^\|/.test(line.trim())) {
      const buf: string[] = [];
      while (i < lines.length && /^\|/.test(lines[i].trim())) {
        buf.push(lines[i].trim());
        i += 1;
      }
      out.push(renderTable(buf));
      continue;
    }

    const heading = /^(#{1,4})\s+(.*)$/.exec(line);
    if (heading) {
      const level = `h${heading[1].length}`;
      out.push(
        `<${level} class="${headCls[level]}">${inline(escapeHtml(heading[2]))}</${level}>`,
      );
      i += 1;
      continue;
    }

    if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
      out.push('<hr class="my-5 border-base-300" />');
      i += 1;
      continue;
    }

    if (/^>\s?/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^>\s?/, ""));
        i += 1;
      }
      out.push(
        `<blockquote class="my-3 border-l-4 border-primary/40 bg-base-200/60 px-4 py-2 text-sm text-base-content/80">${buf
          .map((b) => inline(escapeHtml(b)))
          .join("<br />")}</blockquote>`,
      );
      continue;
    }

    const bullet = /^\s*[-*+]\s+(.*)$/.exec(line);
    if (bullet) {
      const items: string[] = [];
      while (i < lines.length) {
        const m = /^\s*[-*+]\s+(.*)$/.exec(lines[i]);
        if (!m) break;
        items.push(`<li class="leading-7">${inline(escapeHtml(m[1]))}</li>`);
        i += 1;
      }
      out.push(
        `<ul class="my-2 list-disc space-y-1 pl-6 text-sm text-base-content/90 marker:text-primary/60">${items.join("")}</ul>`,
      );
      continue;
    }

    const ordered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (ordered) {
      const items: string[] = [];
      while (i < lines.length) {
        const m = /^\s*\d+[.)]\s+(.*)$/.exec(lines[i]);
        if (!m) break;
        items.push(`<li class="leading-7">${inline(escapeHtml(m[1]))}</li>`);
        i += 1;
      }
      out.push(
        `<ol class="my-2 list-decimal space-y-1 pl-6 text-sm text-base-content/90 marker:font-semibold marker:text-primary/70">${items.join("")}</ol>`,
      );
      continue;
    }

    if (line.trim() === "") {
      i += 1;
      continue;
    }

    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !/^(#{1,4}\s|```|\||>|---\s*$|\s*[-*+]\s|\s*\d+[.)]\s)/.test(lines[i])
    ) {
      para.push(lines[i]);
      i += 1;
    }
    out.push(
      `<p class="my-2 text-sm leading-7 text-base-content/90">${inline(escapeHtml(para.join(" ")))}</p>`,
    );
  }

  return out.join("\n");
}

export const frontMatterLabel: Record<string, string> = {
  execution_id: "Execution ID",
  task_type: "Task Type",
  task_id: "Task ID",
  task_name: "Task Name",
  fire_key: "Idempotency Key",
  status: "Status",
  node: "Node",
  trace_id: "Trace ID",
  fire_at: "Scheduled Fire",
  started_at: "Started",
  finished_at: "Finished",
  duration_ms: "Duration (ms)",
  model: "Model",
  llm_rounds: "LLM Rounds",
  tool_calls: "Tool Calls",
  tokens_prompt: "Prompt Tokens",
  tokens_output: "Output Tokens",
};
