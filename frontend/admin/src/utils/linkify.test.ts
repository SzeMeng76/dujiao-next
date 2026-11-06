import { describe, expect, it } from 'vitest'
import { splitTextLinks } from './linkify'

// 工单正文是用户与客服都能写的自由文本。这批用例锁两件事：
// 1. 常见写法（句末带标点、多个链接、代码块里的地址）能正确切分；
// 2. 危险内容只能是纯文本，绝不会被识别成可点击的链接。
describe('splitTextLinks', () => {
  it('把句子中的链接切出来，标点留在正文里', () => {
    const out = splitTextLinks('Refund done. Please check https://bscscan.com/tx/0x909ae4.')
    expect(out).toEqual([
      { type: 'text', value: 'Refund done. Please check ' },
      { type: 'link', value: 'https://bscscan.com/tx/0x909ae4', href: 'https://bscscan.com/tx/0x909ae4' },
      { type: 'text', value: '.' },
    ])
  })

  it('保留链接自身成对的括号，只去掉多余的右括号', () => {
    const out = splitTextLinks('详见 (https://en.wikipedia.org/wiki/Foo_(bar))')
    expect(out[1]).toEqual({
      type: 'link',
      value: 'https://en.wikipedia.org/wiki/Foo_(bar)',
      href: 'https://en.wikipedia.org/wiki/Foo_(bar)',
    })
    expect(out[2]).toEqual({ type: 'text', value: ')' })
  })

  it('支持一条消息里多个链接和中文标点', () => {
    const out = splitTextLinks('A https://a.com/x，B https://b.com/y。')
    const links = out.filter((s) => s.type === 'link').map((s) => s.value)
    expect(links).toEqual(['https://a.com/x', 'https://b.com/y'])
    expect(out.filter((s) => s.type === 'text').map((s) => s.value)).toEqual(['A ', '，B ', '。'])
  })

  it('没有链接时原样返回一段文本', () => {
    expect(splitTextLinks('okay this wallet is true')).toEqual([
      { type: 'text', value: 'okay this wallet is true' },
    ])
    expect(splitTextLinks('')).toEqual([])
    expect(splitTextLinks(null)).toEqual([])
  })

  it('不把危险协议和裸主机识别成链接', () => {
    for (const payload of [
      'javascript:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      '<img src=x onerror=alert(1)>',
      'https://',
      'www.example.com',
    ]) {
      expect(splitTextLinks(payload).every((s) => s.type === 'text')).toBe(true)
    }
  })

  it('HTML 标签只是文本，不会被当成链接渲染', () => {
    const out = splitTextLinks('<a href="https://evil.com">x</a>')
    // 引号在 URL 字符集之外，所以切出来的是 https://evil.com ，前后标签仍是文本
    expect(out.filter((s) => s.type === 'text').map((s) => s.value).join('')).toContain('<a href="')
    expect(out.some((s) => s.type === 'link' && s.href === 'https://evil.com')).toBe(true)
  })
})
