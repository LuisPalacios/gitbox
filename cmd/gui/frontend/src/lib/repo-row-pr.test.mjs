import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

test('I show pending review text in Simple and both PR counts in Advanced', async (t) => {
  // The credential component test starts a Vite server in another test process.
  const cacheDir = await mkdtemp(join(tmpdir(), 'gitbox-repo-row-vite-'));
  t.after(() => rm(cacheDir, { recursive: true, force: true }));
  const server = await createServer({
    root: fileURLToPath(new URL('../../', import.meta.url)),
    configFile: fileURLToPath(new URL('../../vite.config.ts', import.meta.url)),
    cacheDir,
    logLevel: 'error',
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, ws: false },
  });
  try {
    const { default: RepoRow } = await server.ssrLoadModule('/src/lib/components/RepoRow.svelte');
    const { render } = await server.ssrLoadModule('svelte/server');
    const { languageStore } = await server.ssrLoadModule('/src/lib/i18n.ts');
    const { prsByAccount, prSettings } = await server.ssrLoadModule('/src/lib/stores.ts');

    const repoName = 'owner/project';
    const repoKey = 'source/' + repoName;
    const props = {
      repoName, repoKey, sourceKey: 'source', rowSourceKey: 'source', rowAccountKey: 'account',
      depth: 0, childCount: 0, descendants: [], isContainer: false, rowOpensDetail: true,
      state: { status: 'clean' }, deleteMode: false, fetchingRepos: {}, collapsedRepos: new Set(),
      workspaceChoices: new Set(), configEditors: [], configProfiles: [], configAIHarnesses: [],
    };
    const authored = [{ number: 1, repoFull: repoName, title: 'Authored fixture' }];
    const reviewRequested = [
      { number: 2, repoFull: repoName, title: 'First review fixture' },
      { number: 3, repoFull: repoName, title: 'Second review fixture' },
    ];
    prsByAccount.set({ account: { [repoName]: { authored, reviewRequested } } });

    for (const [language, label] of [['en', 'Review requested'], ['es', 'Revisión pendiente']]) {
      languageStore.set(language);
      const simple = render(RepoRow, { props: { ...props, simple: true } }).body;
      assert.equal(simple.split(label).length - 1, 1);
      assert.doesNotMatch(simple, /pr-badge-authored|pr-badge-review/);
    }

    const openReview = render(RepoRow, {
      props: { ...props, simple: true, openPRPopover: repoKey + '|review' },
    }).body;
    assert.match(openReview, /class="pr-popover\b/);
    assert.match(openReview, /First review fixture/);
    assert.match(openReview, /Second review fixture/);
    assert.doesNotMatch(openReview, /Authored fixture/);

    const advanced = render(RepoRow, { props: { ...props, simple: false } }).body.replace(/<!--[\s\S]*?-->/g, '');
    assert.match(advanced, /class="[^"]*pr-badge-authored[^"]*"[^>]*>\s*📝 1\s*<\/button>/u);
    assert.match(advanced, /class="[^"]*pr-badge-review[^"]*"[^>]*>\s*👀 2\s*<\/button>/u);
    assert.doesNotMatch(advanced, /pr-review-simple/);

    prsByAccount.set({ account: { [repoName]: { authored, reviewRequested: [] } } });
    const authoredOnly = render(RepoRow, { props: { ...props, simple: true } }).body;
    assert.doesNotMatch(authoredOnly, /pr-review-simple|pr-badge-authored/);

    prsByAccount.set({ account: { [repoName]: { authored, reviewRequested } } });
    prSettings.set({ enabled: false, includeDrafts: true });
    const disabled = render(RepoRow, { props: { ...props, simple: true } }).body;
    assert.doesNotMatch(disabled, /pr-review-simple/);
  } finally {
    await server.close();
  }
});
