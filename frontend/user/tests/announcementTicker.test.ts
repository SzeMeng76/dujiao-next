import test from 'node:test'
import assert from 'node:assert/strict'
import { announcementToTickerItems } from '../src/utils/announcementTicker.ts'

test('splits paragraphs, list items and line breaks into separate items', () => {
  const html = '<p>质保与发货</p><ul><li>自动发货</li><li>人工发货</li></ul><p>第一行<br>第二行</p>'
  assert.deepEqual(announcementToTickerItems(html), ['质保与发货', '自动发货', '人工发货', '第一行', '第二行'])
})

test('strips tags, decodes entities once and drops empty lines', () => {
  const html = '<p><strong>A&nbsp;&amp;&nbsp;B</strong></p><p>&nbsp;</p><p>&amp;lt;</p>'
  assert.deepEqual(announcementToTickerItems(html), ['A & B', '&lt;'])
})

test('returns an empty list for empty input and caps the item count', () => {
  assert.deepEqual(announcementToTickerItems(''), [])
  const many = Array.from({ length: 30 }, (_, i) => `<p>${i}</p>`).join('')
  assert.equal(announcementToTickerItems(many).length, 20)
  assert.equal(announcementToTickerItems(many, 5).length, 5)
})
