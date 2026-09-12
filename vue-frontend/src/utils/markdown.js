import MarkdownIt from 'markdown-it'

// Keep model/user HTML as text; the parser validates Markdown link protocols.
const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true })

export function renderMarkdown(text) {
  return markdown.render(text == null ? '' : String(text))
}
