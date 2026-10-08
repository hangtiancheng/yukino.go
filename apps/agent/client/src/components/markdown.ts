import DOMPurify from "dompurify";
import hljs from "highlight.js/lib/common";
import MarkdownIt from "markdown-it";
import "highlight.js/styles/github.css";

/**
 * Fence info strings are author-controlled, so only a conservative token
 * charset is echoed back into the generated class attribute.
 */
const LANG_RE = /^[A-Za-z0-9_+-]+$/;

const markdownIt = new MarkdownIt({
  // Keep raw HTML disabled: model output is untrusted, and DOMPurify below is
  // the single sanitization point.
  html: false,
  // Single newlines become <br>. Chat replies are written with soft line breaks
  // that would otherwise collapse into one run-on paragraph.
  breaks: true,
  // Bare URLs become links.
  linkify: true,
  highlight(code, lang) {
    const language =
      lang && LANG_RE.test(lang) && hljs.getLanguage(lang) ? lang : "plaintext";
    try {
      const highlighted = hljs.highlight(code, {
        language,
        ignoreIllegals: true,
      }).value;
      // Returning a string that starts with <pre> makes markdown-it emit it
      // verbatim instead of wrapping it again.
      return `<pre><code class="hljs language-${language}">${highlighted}</code></pre>`;
    } catch {
      // Fall back to markdown-it's own escaping.
      return "";
    }
  },
});

/**
 * Renders markdown to sanitized HTML, safe to inject via unsafeHTML.
 *
 * @param {string} value The markdown source to render.
 * @returns {string} The sanitized HTML string.
 */
export function renderMarkdown(value: string): string {
  return DOMPurify.sanitize(markdownIt.render(value));
}
