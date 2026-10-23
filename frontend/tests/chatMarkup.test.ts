// Dev chat markup and keyboard (src/lib/chatMarkup.ts).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  buttonRows,
  parseHtml,
  parseMarkdown,
  renderMarkup,
  safeHref,
} from '../src/lib/chatMarkup.ts'

test('markdown: bold, italic, code, link', () => {
  assert.deepEqual(parseMarkdown('**Hi!** I am *bot* `x` [doc](https://e.org/a)'), [
    { text: 'Hi!', bold: true },
    { text: ' I am ' },
    { text: 'bot', italic: true },
    { text: ' ' },
    { text: 'x', code: true },
    { text: ' ' },
    { text: 'doc', href: 'https://e.org/a' },
  ])
})

test('markdown: unpaired marker stays literal, nested styles combine', () => {
  assert.deepEqual(parseMarkdown('2*3 = 6'), [{ text: '2*3 = 6' }])
  assert.deepEqual(parseMarkdown('**a _b_**'), [
    { text: 'a ', bold: true },
    { text: 'b', bold: true, italic: true },
  ])
  assert.deepEqual(parseMarkdown('`**x**`'), [{ text: '**x**', code: true }])
})

test('markdown: unsafe link keeps its text, no href', () => {
  assert.deepEqual(parseMarkdown('[click](javascript:void)'), [{ text: 'click' }])
  assert.deepEqual(parseMarkdown('[a](data:x)'), [{ text: 'a' }])
})

test('html: tags become styles, unknown tags and entities are text', () => {
  assert.deepEqual(parseHtml('<b>Bold</b> &lt;3 <script>x</script><br/>end'), [
    { text: 'Bold', bold: true },
    { text: ' <3 <script>x</script>\nend' },
  ])
  assert.deepEqual(parseHtml('<a href="https://e.org">e</a> <a href="javascript:x">j</a>'), [
    { text: 'e', href: 'https://e.org' },
    { text: ' j' },
  ])
})

test('plain format is left as is', () => {
  assert.deepEqual(renderMarkup('**not bold**', ''), [{ text: '**not bold**' }])
  assert.deepEqual(renderMarkup('', 'markdown'), [])
  assert.equal(safeHref(' https://x.y/z '), 'https://x.y/z')
  assert.equal(safeHref('ftp://x'), undefined)
})

test('button rows: actions by type, empty rows dropped', () => {
  const rows = buttonRows([
    [
      { text: 'Yes', type: 'callback', payload: 'y' },
      { text: 'Site', type: 'link', url: 'https://e.org' },
    ],
    [],
    [
      { text: 'When is pay day?', type: 'message' },
      { text: 'Phone', type: 'request_contact' },
      { text: 'Bad', type: 'link', url: 'javascript:x' },
    ],
  ])
  assert.equal(rows.length, 2)
  assert.deepEqual(
    rows.map((r) => r.map((b) => b.action)),
    [
      ['press', 'open'],
      ['send', 'none', 'none'],
    ],
  )
  assert.equal(rows[0][0].payload, 'y')
  assert.equal(rows[0][1].url, 'https://e.org')
  assert.deepEqual(buttonRows(null), [])
})
