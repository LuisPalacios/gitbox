import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mount } from '../testing/mount.mjs'

const contentWidth = 600

// Apply the real narrow-content CSS to the mounted fixture. jsdom parses
// container rules but has no layout engine to evaluate them.
function applyContentRules(window, width) {
  for (const sheet of window.document.styleSheets) {
    for (let i = sheet.cssRules.length - 1; i >= 0; i--) {
      const rule = sheet.cssRules[i]
      if (rule.containerName !== 'content') continue
      const match = /^\(max-width:\s*(\d+)px\)$/.exec(rule.containerQuery)
      assert.ok(match, `Unhandled content query: ${rule.conditionText}`)
      if (width > Number(match[1])) continue
      sheet.deleteRule(i)
      for (const child of [...rule.cssRules].reverse()) sheet.insertRule(child.cssText, i)
    }
  }
}

const graphemes = new Intl.Segmenter()
const number = value => Number.parseFloat(value) || 0

// Estimate the untruncated width of the mounted row at a fixed-width
// fallback of 0.62em per character. Read spacing, box sizes and visibility
// from the stylesheet. A two-line status needs room for its wider line.
function intrinsicWidth(window, element, inheritedFont = 16) {
  const style = window.getComputedStyle(element)
  if (style.display === 'none' || style.position === 'absolute') return 0
  const margin = number(style.marginLeft) + number(style.marginRight)
  const left = element.matches('.repo-row')
    ? number(style.getPropertyValue('--row-pad').match(/^calc\((\d+px)/)?.[1])
      + number(style.getPropertyValue('--depth')) * number(style.getPropertyValue('--indent'))
    : number(style.paddingLeft)
  const edges = left + number(style.paddingRight) + number(style.borderLeftWidth) + number(style.borderRightWidth)
  const declaredWidth = /^var\((--[^)]+)\)$/.exec(style.width)?.[1]
  const fixed = number(declaredWidth ? style.getPropertyValue(declaredWidth) : style.width)
  if (fixed) return fixed + margin + (style.boxSizing === 'border-box' ? 0 : edges)
  const minimum = style.minWidth.endsWith('rem')
    ? number(style.minWidth) * 16
    : number(style.minWidth)
  // The project name can ellipsize by design. Preserve its CSS minimum and
  // budget the remaining room for the full status, captions and actions.
  if (element.matches('.repo-name')) {
    assert.ok(minimum > 0)
    assert.equal(style.textOverflow, 'ellipsis')
    return Math.max(minimum, edges) + margin
  }
  const font = number(style.fontSize) || inheritedFont
  const parts = [...element.childNodes].flatMap(node => {
    if (node.nodeType === window.Node.ELEMENT_NODE) {
      const visible = window.getComputedStyle(node).display !== 'none'
      return visible ? [intrinsicWidth(window, node, font)] : []
    }
    const text = node.nodeType === window.Node.TEXT_NODE ? node.textContent.replace(/\s+/g, ' ').trim() : ''
    return text ? [[...graphemes.segment(text)].length * font * 0.62] : []
  })
  const size = style.flexDirection === 'column'
    ? Math.max(0, ...parts)
    : parts.reduce((total, width) => total + width, 0) + Math.max(0, parts.length - 1) * number(style.columnGap || style.gap)
  return Math.max(size + edges, minimum) + margin
}

const states = [
  ['clean'], ['dirty', { modified: 1 }], ['dirty', { modified: 27 }],
  ['ahead', { ahead: 4 }], ['behind', { behind: 2 }], ['diverged', { ahead: 1, behind: 2 }],
  ['conflict'], ['error'], ['upstream gone'], ['not cloned'], ['root offline'],
  ['no upstream', { isDefault: true }], ['no upstream', { isDefault: false }], ['no upstream'],
  ['unknown'], ['pending'], ['cloning', { progress: 68 }], ['syncing', { progress: 100 }],
]

for (const [lang, updateLabel, downloadLabel] of [
  ['en', 'Update', 'Download'],
  ['es', 'Actualizar', 'Descargar'],
]) {
  test(`Simple RepoRow labels fit in 600px of content in ${lang}`, async t => {
    const { RepoRow, languageStore, render, window } = await mount(t, contentWidth)
    languageStore.set(lang)
    applyContentRules(window, contentWidth)
    const content = window.document.createElement('main')
    content.className = 'content'
    content.style.width = `${contentWidth}px`
    const view = window.document.createElement('section')
    view.className = 'view-scroll'
    window.document.body.append(content)
    content.append(view)
    const viewStyle = window.getComputedStyle(view)
    const availableWidth = number(window.getComputedStyle(content).width)
      - number(viewStyle.paddingLeft) - number(viewStyle.paddingRight)
    assert.equal(availableWidth, 576)

    const checkedAt = new Date(Date.now() - 3 * 60_000).toISOString()
    for (const [status, overrides] of states) {
      const fixture = render(RepoRow, {
        target: view,
        props: {
          sourceKey: 'fixture', rowSourceKey: 'fixture', rowAccountKey: 'demo-account',
          repoName: 'example/project-fixture', repoKey: 'fixture/example/project-fixture',
          depth: 0, childCount: 0, descendants: [], isContainer: false,
          state: { status, checkedAt, modified: 0, untracked: 0, ahead: 0, behind: 0, progress: 0, ...overrides },
          showAccount: true, rowOpensDetail: false, simple: true, deleteMode: false, fetchingRepos: {},
          collapsedRepos: new Set(), workspaceChoices: new Set(), revealLabel: '',
          configEditors: [{ id: 'code', name: 'Editor', command: 'code' }],
          configAIHarnesses: [{ name: 'Example', command: 'example' }],
          configProfiles: [{ id: 'terminal', name: 'Terminal' }],
          actionMenuRepo: null, openPRPopover: null,
        },
      })
      const row = view.querySelector('.simple-row')
      assert.ok(row)
      const label = row.querySelector('.words-short') ?? row.querySelector('.status-text, .progress-pct')
      assert.ok(label?.textContent.trim(), `missing ${lang} label for ${status}`)
      assert.notEqual(window.getComputedStyle(label).display, 'none')
      if (status === 'behind') assert.equal(row.querySelector('.btn-action')?.textContent, updateLabel)
      if (status === 'not cloned') assert.equal(row.querySelector('.btn-action')?.textContent, downloadLabel)
      const size = intrinsicWidth(window, row)
      assert.ok(size >= 100 && size <= availableWidth, `${lang} ${status} needs ${Math.ceil(size)}px of ${availableWidth}px`)
      // Negative control. A stretched translated label must fail the same
      // budget, even though jsdom would report zero-length DOM rectangles.
      if (status === 'behind') {
        label.textContent = label.textContent.repeat(20)
        assert.ok(intrinsicWidth(window, row) > availableWidth)
      }
      await fixture.unmount()
    }
  })
}
