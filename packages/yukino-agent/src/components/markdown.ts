import DOMPurify from "dompurify";
import MarkdownIt from "markdown-it";

const markdownIt = new MarkdownIt();

/**
 * Renders markdown to sanitized HTML, safe to inject via unsafeHTML.
 *
 * @param {string} value The markdown source to render.
 * @returns {string} The sanitized HTML string.
 */
export function renderMarkdown(value: string): string {
  return DOMPurify.sanitize(markdownIt.render(value));
}
