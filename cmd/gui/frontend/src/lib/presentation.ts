// The dashboard uses the same classification for words, actions and counts.
// Account credential health and the age of a network check are independent.
export const ATTENTION_GROUPS = ['help', 'local', 'behind', 'notCloned'] as const;
export const CALM_RANK = ATTENTION_GROUPS.length;

type SimpleKind = 'saved' | 'wip' | 'unpushed' | 'update' | 'online' | 'offline' | 'help' | 'checking' | 'busy';
type Mode = 'simple' | 'advanced';
type Action = 'update' | 'open' | 'fix' | 'clone' | 'none';

interface State {
  status: string;
  repairWarnings?: string[];
  isDefault?: boolean;
  ahead?: number;
  behind?: number;
  modified?: number;
  untracked?: number;
}

interface Rule {
  kind: SimpleKind;
  attention: number;
  advancedKey: string;
  advancedColour?: string;
  simpleKey?: string;
  badges?: boolean;
}

const rules: Record<string, Rule> = {
  clean:           { kind: 'saved',    attention: CALM_RANK, advancedKey: 'status.upToDate' },
  dirty:           { kind: 'wip',      attention: 1, advancedKey: 'status.localChanges' },
  ahead:           { kind: 'unpushed', attention: 1, advancedKey: 'status.ahead' },
  behind:          { kind: 'update',   attention: 2, advancedKey: 'status.behind' },
  diverged:        { kind: 'help',     attention: 0, advancedKey: 'status.diverged', badges: true },
  conflict:        { kind: 'help',     attention: 0, advancedKey: 'status.conflict', badges: true },
  error:           { kind: 'help',     attention: 0, advancedKey: 'status.error' },
  'upstream gone': { kind: 'help',     attention: 0, advancedKey: 'status.upstreamGone' },
  'not cloned':    { kind: 'online',   attention: 3, advancedKey: 'status.notCloned' },
  'no upstream':   { kind: 'unpushed', attention: 1, advancedKey: 'repo.noUpstream' },
  'root offline':  { kind: 'offline',  attention: CALM_RANK, advancedKey: 'status.rootOffline' },
  syncing:         { kind: 'busy',     attention: 2, advancedKey: 'simple.checking', advancedColour: 'not cloned' },
  cloning:         { kind: 'busy',     attention: 3, advancedKey: 'simple.checking', advancedColour: 'not cloned' },
  fetching:        { kind: 'busy',     attention: CALM_RANK, advancedKey: 'simple.checking', advancedColour: 'not cloned' },
  unknown:         { kind: 'checking', attention: CALM_RANK, advancedKey: 'simple.checking', advancedColour: 'not cloned' },
};
const localBranch: Rule = { kind: 'saved', attention: CALM_RANK, advancedKey: 'repo.localBranch', simpleKey: 'repo.localBranch', advancedColour: 'clean' };
const fallback: Rule = { kind: 'help', attention: CALM_RANK, advancedKey: 'simple.checking', advancedColour: 'not cloned' };

const kinds: Record<SimpleKind, { colour: string; action: Action }> = {
  saved:    { colour: 'clean', action: 'open' },
  wip:      { colour: 'dirty', action: 'open' },
  unpushed: { colour: 'ahead', action: 'open' },
  update:   { colour: 'behind', action: 'update' },
  online:   { colour: 'not cloned', action: 'clone' },
  offline:  { colour: 'root offline', action: 'none' },
  help:     { colour: 'error', action: 'fix' },
  checking: { colour: 'not cloned', action: 'none' },
  busy:     { colour: 'syncing', action: 'none' },
};

export interface Presentation {
  kind: SimpleKind;
  attention: number;
  primaryAction: Action;
  stale: boolean;
  labelKey: string;
  colour: string;
  badges: boolean;
  vars?: Record<string, number>;
  note?: string;
  noteVars?: Record<string, number>;
  short?: string;
  shortVars?: Record<string, number>;
}

function ruleFor(state: State): Rule {
  if (state.status === 'no upstream') {
    if (state.isDefault === undefined) return rules.unknown;
    if (!state.isDefault) return localBranch;
  }
  return Object.hasOwn(rules, state.status) ? rules[state.status] : fallback;
}

export function derivePresentation(state: State = { status: 'unknown' }, mode: Mode = 'advanced'): Presentation {
  const rule = ruleFor(state);
  const { kind, attention } = rule;
  const simple = mode === 'simple';
  const presentation: Presentation = {
    kind, attention: state.repairWarnings?.length ? 0 : attention, primaryAction: kinds[kind].action, stale: kind === 'checking',
    labelKey: simple ? rule.simpleKey ?? `simple.${kind === 'busy' ? 'checking' : kind}` : rule.advancedKey,
    colour: simple ? kinds[kind].colour : rule.advancedColour ?? state.status,
    badges: !simple && !!rule.badges,
  };
  if (kind === 'wip') {
    const changes = { n: (state.modified ?? 0) + (state.untracked ?? 0) };
    const one = changes.n === 1;
    if (simple) {
      presentation.note = one ? 'simple.wipSafeOne' : 'simple.wipSafe';
      presentation.short = one ? 'simple.wipShortOne' : 'simple.wipShort';
      presentation.noteVars = changes;
      presentation.shortVars = changes;
    } else {
      presentation.labelKey = one ? 'status.localChange' : rule.advancedKey;
      presentation.note = one ? 'status.protectedOne' : 'status.protected';
      presentation.vars = changes;
    }
  } else if (!simple && state.status === 'ahead') {
    presentation.vars = { n: state.ahead ?? 0 };
  } else if (!simple && state.status === 'behind') {
    presentation.vars = { n: state.behind ?? 0 };
  }
  return presentation;
}
