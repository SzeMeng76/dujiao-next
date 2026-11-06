/**
 * 工单消息里的链接切分工具。与 admin 端 `utils/linkify.ts` 同一份实现 ——
 * 两个前端是独立的包，没有共享目录，改动时两边同步。
 *
 * 刻意不拼 HTML 交给 v-html：工单正文是用户与客服双向写入的自由文本，
 * 走 HTML 就可能执行注入内容。切分后由模板渲染 <a>，只有 http(s) 地址会成链接。
 */
export interface TextLinkSegment {
  type: 'text' | 'link'
  value: string
  href?: string
}

const URL_RE = /https?:\/\/[^\s<>"'`，。；：！？、（）【】《》「」『』…]+/gi

function trimTrailingPunctuation(raw: string): { url: string; tail: string } {
  let url = raw
  let tail = ''
  for (;;) {
    const last = url[url.length - 1]
    if (!last) break
    if (/[.,;:!?，。；：！？、）】》」』…]/.test(last)) {
      tail = last + tail
      url = url.slice(0, -1)
      continue
    }
    if (last === ')' && (url.match(/\(/g)?.length ?? 0) < (url.match(/\)/g)?.length ?? 0)) {
      tail = last + tail
      url = url.slice(0, -1)
      continue
    }
    break
  }
  return { url, tail }
}

export function splitTextLinks(text: string | null | undefined): TextLinkSegment[] {
  if (!text) return []
  const segments: TextLinkSegment[] = []
  let cursor = 0

  URL_RE.lastIndex = 0
  for (let match = URL_RE.exec(text); match; match = URL_RE.exec(text)) {
    const raw = match[0]
    const { url, tail } = trimTrailingPunctuation(raw)
    if (!/^https?:\/\/[^/?#\s]+/i.test(url)) continue

    if (match.index > cursor) {
      segments.push({ type: 'text', value: text.slice(cursor, match.index) })
    }
    segments.push({ type: 'link', value: url, href: url })
    if (tail) segments.push({ type: 'text', value: tail })
    cursor = match.index + raw.length
  }

  if (cursor < text.length) segments.push({ type: 'text', value: text.slice(cursor) })
  return segments
}
