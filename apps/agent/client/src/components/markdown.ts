import DOMPurify from "dompurify";
import hljs from "highlight.js/lib/common";
import MarkdownIt from "markdown-it";
import "highlight.js/styles/github.css";

const LANG_RE = /^[A-Za-z0-9_+-]+$/;

const markdownIt = new MarkdownIt({
  html: false,
  breaks: true,
  linkify: true,
  highlight(code, lang) {
    const language =
      lang && LANG_RE.test(lang) && hljs.getLanguage(lang) ? lang : "plaintext";
    try {
      const highlighted = hljs.highlight(code, {
        language,
        ignoreIllegals: true,
      }).value;
      return `<pre><code class="hljs language-${language}">${highlighted}</code></pre>`;
    } catch {
      return "";
    }
  },
});

export function renderMarkdown(value: string): string {
  return DOMPurify.sanitize(markdownIt.render(value));
}
