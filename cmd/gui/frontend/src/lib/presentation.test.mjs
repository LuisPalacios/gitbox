import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// I pin the old row words, ring membership, ranks, and colours independently
// of the classifier. Both modes use the same rank and saved count.
const rows = [
  // status, kind, rank, Advanced key, Advanced colour, Simple colour, action
  ['clean', 'saved', 4, 'status.upToDate', 'clean', 'clean', 'open'],
  ['dirty', 'wip', 1, 'status.localChanges', 'dirty', 'dirty', 'open'],
  ['behind', 'update', 2, 'status.behind', 'behind', 'behind', 'update'],
  ['ahead', 'unpushed', 1, 'status.ahead', 'ahead', 'ahead', 'open'],
  ['diverged', 'help', 0, 'status.diverged', 'diverged', 'error', 'fix'],
  ['conflict', 'help', 0, 'status.conflict', 'conflict', 'error', 'fix'],
  ['not cloned', 'online', 3, 'status.notCloned', 'not cloned', 'not cloned', 'clone'],
  ['no upstream', 'unpushed', 1, 'repo.noUpstream', 'no upstream', 'ahead', 'open'],
  ['upstream gone', 'help', 0, 'status.upstreamGone', 'upstream gone', 'error', 'fix'],
  ['root offline', 'offline', 4, 'status.rootOffline', 'root offline', 'root offline', 'none'],
  ['error', 'help', 0, 'status.error', 'error', 'error', 'fix'],
  ['syncing', 'busy', 2, 'simple.checking', 'not cloned', 'syncing', 'none'],
  ['cloning', 'busy', 3, 'simple.checking', 'not cloned', 'syncing', 'none'],
  ['fetching', 'busy', 4, 'simple.checking', 'not cloned', 'syncing', 'none'],
  ['unknown', 'checking', 4, 'simple.checking', 'not cloned', 'not cloned', 'none'],
];

const source = await readFile(new URL('./presentation.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { derivePresentation } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('I classify every status in both modes for each branch identity', () => {
  for (const [status, kind, rank, advancedKey, advancedColour, simpleColour, action] of rows) {
    for (const mode of ['simple', 'advanced']) {
      assert.equal(derivePresentation({ status, repairWarnings: ['identity'] }, mode).attention, 0);
    }
    for (const isDefault of [true, false, undefined]) {
      for (const mode of ['simple', 'advanced']) {
        const context = JSON.stringify({ status, isDefault, mode });
        const p = derivePresentation({ status, isDefault, modified: 2, untracked: 1 }, mode);
        if (status === 'no upstream' && isDefault !== true) {
          // A known feature branch is local and calm. While its identity
          // is missing, the row counts only towards the total.
          const local = isDefault === false;
          assert.deepEqual(p, {
            kind: local ? 'saved' : 'checking', attention: 4, primaryAction: local ? 'open' : 'none',
            labelKey: local ? 'repo.localBranch' : 'simple.checking', colour: local ? 'clean' : 'not cloned',
            badges: false, stale: !local,
          }, context);
          continue;
        }
        const simple = mode === 'simple';
        assert.equal(p.kind, kind, context);
        assert.equal(p.attention, rank, context);
        assert.equal(p.primaryAction, action, context);
        assert.equal(p.labelKey, simple ? `simple.${kind === 'busy' ? 'checking' : kind}` : advancedKey, context);
        assert.equal(p.colour, simple ? simpleColour : advancedColour, context);
        assert.equal(p.stale, status === 'unknown', context);
        assert.equal(p.badges, !simple && ['diverged', 'conflict'].includes(status), context);
      }
    }
  }
});

test('I keep counts, singular forms and narrow labels for each mode', () => {
  for (const n of [0, 1, 3]) {
    const state = { status: 'dirty', modified: Math.min(1, n), untracked: Math.max(0, n - 1) };
    const simple = derivePresentation(state, 'simple');
    assert.equal(simple.note, n === 1 ? 'simple.wipSafeOne' : 'simple.wipSafe');
    assert.equal(simple.short, n === 1 ? 'simple.wipShortOne' : 'simple.wipShort');
    assert.deepEqual(simple.noteVars, { n });
    assert.deepEqual(simple.shortVars, { n });
    const advanced = derivePresentation(state);
    assert.equal(advanced.labelKey, n === 1 ? 'status.localChange' : 'status.localChanges');
    assert.equal(advanced.note, n === 1 ? 'status.protectedOne' : 'status.protected');
    assert.deepEqual(advanced.vars, { n });
  }
  for (const status of ['ahead', 'behind']) {
    assert.deepEqual(derivePresentation({ status, [status]: 5 }).vars, { n: 5 });
  }
});
