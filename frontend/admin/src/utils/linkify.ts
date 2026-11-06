/**
 * 把一段纯文本切成「文本片段 / 链接片段」，供模板用 v-for 渲染。
 *
 * 工单消息里客服常贴交易哈希链接（bscscan 之类），用户希望能直接点开。
 * 这里刻意不把内容拼成 HTML 交给 v-html：工单正文是用户与客服双向写入的自由文本，
 * 一旦走 HTML，`<img onerror>` 这类内容就能执行，而 admin 侧同源、localStorage 里有 token。
 * 切分后由模板自己渲染 <a>，只有这里认定为 http(s) 的地址才会变成链接。
 */
export interface TextLinkSegment {
  type: 'text' | 'link'
  /** 原文片段；type 为 link 时是去掉尾随标点的完整地址。 */
  value: string
  /** 仅 type 为 link 时存在，可直接用作 <a href>。 */
  href?: string
}

/**
 * 只认 http/https。不匹配 www. 开头的裸域名：补全成 http:// 会让用户点进明文站点，
 * 补成 https:// 又可能在对方不支持时打不开，交给写的人显式写协议更安全。
 *
 * 中文标点必须一起当分隔符：中文句子里的逗号/句号后面通常紧跟空格，
 * 只按空白切会把「https://a.com/x，后面还有字」整段吞成链接。
 */
const URL_RE = /https?:\/\/[^\s<>"'`，。；：！？、（）【】《》「」『』…]+/gi

/**
 * 地址末尾常跟标点，属于句子而不是链接的一部分（"见 https://x.com/tx/0xabc。"）。
 * 成对括号要保留，例如维基式链接本身带 ")"。
 */
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
    // 右括号只在左括号没有配对时才是多余的。
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
    // 只剩 "https://" 这种没有主机名的，当普通文本。
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
