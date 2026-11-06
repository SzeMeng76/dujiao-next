import test from 'node:test'
import assert from 'node:assert/strict'
import { splitTextLinks } from '../src/utils/linkify.ts'

// 工单正文是用户与客服都能写的自由文本。这批用例锁两件事：
// 1. 常见写法（句末带标点、多个链接）能正确切分；
// 2. 危险内容只能是纯文本，绝不会被识别成可点击的链接。
test('splits a link out of a sentence and leaves the punctuation in the text', () => {
  assert.deepEqual(splitTextLinks('Refund done. Please check https://bscscan.com/tx/0x909ae4.'), [
    { type: 'text', value: 'Refund done. Please check ' },
    { type: 'link', value: 'https://bscscan.com/tx/0x909ae4', href: 'https://bscscan.com/tx/0x909ae4' },
    { type: 'text', value: '.' },
  ])
})

test('keeps balanced parentheses that belong to the link', () => {
  const out = splitTextLinks('详见 (https://en.wikipedia.org/wiki/Foo_(bar))')
  assert.deepEqual(out[1], {
    type: 'link',
    value: 'https://en.wikipedia.org/wiki/Foo_(bar)',
    href: 'https://en.wikipedia.org/wiki/Foo_(bar)',
  })
  assert.deepEqual(out[2], { type: 'text', value: ')' })
})

test('handles several links and CJK punctuation', () => {
  const out = splitTextLinks('A https://a.com/x，B https://b.com/y。')
  assert.deepEqual(
    out.filter((s) => s.type === 'link').map((s) => s.value),
    ['https://a.com/x', 'https://b.com/y'],
  )
  assert.deepEqual(
    out.filter((s) => s.type === 'text').map((s) => s.value),
    ['A ', '，B ', '。'],
  )
})

test('returns the text untouched when there is no link', () => {
  assert.deepEqual(splitTextLinks('okay this wallet is true'), [
    { type: 'text', value: 'okay this wallet is true' },
  ])
  assert.deepEqual(splitTextLinks(''), [])
  assert.deepEqual(splitTextLinks(null), [])
})

test('never turns dangerous protocols or bare hosts into links', () => {
  for (const payload of [
    'javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    'https://',
    'www.example.com',
  ]) {
    assert.equal(splitTextLinks(payload).every((s) => s.type === 'text'), true, payload)
  }
})
