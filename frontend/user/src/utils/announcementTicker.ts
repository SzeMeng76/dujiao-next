// 把公告富文本 HTML 转成跑马灯用的纯文本条目：
// 每个段落 / 列表项 / 标题 / 换行各算一条，去掉所有标签，空行丢弃。
// 结果只作为文本节点渲染（{{ }}），不会再被当作 HTML 解析。

const ENTITIES: Record<string, string> = {
  '&nbsp;': ' ',
  '&lt;': '<',
  '&gt;': '>',
  '&quot;': '"',
  '&#39;': "'",
  '&amp;': '&',
}

export function announcementToTickerItems(html: string, max = 20): string[] {
  if (!html) return []
  const text = String(html)
    .replace(/<\s*br\s*\/?>/gi, '\n')
    .replace(/<\/\s*(p|div|li|h[1-6]|blockquote|pre|tr)\s*>/gi, '\n')
    .replace(/<[^>]*>/g, '')
    .replace(/&(?:nbsp|lt|gt|quot|#39|amp);/g, (m) => ENTITIES[m] ?? m)
  return text
    .split(/\n+/)
    .map((line) => line.replace(/\s+/g, ' ').trim())
    .filter(Boolean)
    .slice(0, max)
}
