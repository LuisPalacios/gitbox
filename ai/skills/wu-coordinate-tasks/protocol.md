# Coordination protocol

Read this with [SKILL.md](SKILL.md). I use the helper as a local journal and exclusion mechanism, not as an agent runner, permission system or test executor. Only the supervisor writes its batch records; workers send evidence and requests through messaging.

## Registry requests

Run `uv run {{SKILLS}}/wu-coordinate-tasks/scripts/coordinate.py --repo <checkout> status` before mutations. It returns sessions, claims, resources, evidence and actions. A missing registry returns empty lists without creating it. Use `apply --request-file <file>` with one JSON request, or `apply --request-file -` with JSON on stdin. Write request files through a file-writing capability; do not interpolate arbitrary JSON into shell commands.

Start a batch with a unique owner identifier:

```json
{"op":"start","owner":"batch-example","issues":[42,43,44],"work_unit":40}
```

`work_unit` is mandatory for execution batches. The supervisor first verifies every selected issue is a pending native direct task of that work unit; this offline helper cannot authenticate GitHub relationships. It rejects the container as its own task and another active batch with unreleased claims for the same work unit. Different work units may run concurrently, subject to shared issue/resource exclusions. The helper returns the generation. A transaction reserves all issues or none; repeating the same start is idempotent while the same issue set remains active. An existing reservation is a conflict regardless of heartbeat age. For declared external work, use `{"op":"start","owner":"external-example","issues":[50],"external":true}` without `work_unit`. These unscoped claims only exclude work; never launch workers from them. Use a separate owner identifier and mark its claims blocked with a note; do not impersonate or replace an existing owner.

Every later request includes `owner` and `generation`. Issue operations also include `issue`. These illustrative SHAs must be replaced by actual full object IDs:

```json
{
  "op":"update","owner":"batch-example","generation":1,"issue":42,
  "state":"working",
  "detail":{
    "branch":"fix/42-example","worktree":"<absolute-worktree>","worker":"<agent-id>",
    "head":"1111111111111111111111111111111111111111",
    "base":"2222222222222222222222222222222222222222",
    "criteria":["focused-tests","health","behavioral-smoke"],
    "note":"Scope, acceptance criteria, external overlaps and current-session authority"
  }
}
```

Allowed states are `reserved`, `working`, `blocked`, `verifying`, `publication-pending`, `pr-open` and `ready`; `release` sets `released`. `detail` accepts `branch`, `worktree`, `worker`, `head`, `base`, `criteria`, `note`, `pr`, `roadmap` (legacy metadata only; session scope is immutable). Changing head, base or criteria deletes prior evidence and sets `verifying`. Store platform-specific criteria separately when several platforms must pass. Before publication, `base` must match the current target-branch head after synchronization.

```json
{
  "op":"evidence","owner":"batch-example","generation":1,"issue":42,
  "criterion":"focused-tests",
  "head":"1111111111111111111111111111111111111111",
  "base":"2222222222222222222222222222222222222222",
  "result":"PASS",
  "detail":{"command":"<actual-command>","platform":"<platform>","fixture":"<isolated-fixture>","outcome":"<observed-result>","evidence":"<private-log>"}
}
```

`result` is `PASS`, `FAIL` or `PENDING`. `{"op":"check","owner":"batch-example","generation":1,"issue":42}` rejects absent, failed, pending or stale required evidence. It cannot detect fabricated results, uncommitted changes or an unrecorded remote-base change; the supervisor must inspect those before recording/publishing. State `ready` also checks the recorded evidence, but the local gate on the published SHA remains the supervisor's responsibility.

## Resources and recovery

Acquire a named exclusive resource:

```json
{"op":"acquire","owner":"batch-example","generation":1,"resource":"gui-test"}
```

Use `release-resource` with the same fields only after its process/operation stops. Names include `gui-test`, `heavy-checks`, `integration-fixture`, `real-config`, `github-cli`, `staging:<destination-id>` and `roadmap:<issue-number>`. Reacquiring within the same owner is idempotent, so the supervisor must serialize workers within its own batch; the registry excludes other owners. The `github-cli` resource covers account selection and the complete following command. Do not release it between them.

Send `heartbeat` with owner/generation at milestones. A paused or dead supervisor retains all reservations. Inspect actual agents/processes, worktrees and remote state before takeover; never terminate another session or discard its changes automatically.

```json
{"op":"takeover","owner":"batch-example","generation":1,"new_owner":"batch-recovery","work_unit":40,"confirmed_stopped":true}
```

A scoped takeover requires the explicit matching `work_unit` and fresh validation of native task membership. Unscoped external or legacy records do not authorize execution; account for their work before creating a new scoped batch.

Takeover increments the generation, transfers claims/resources/journal, blocks unfinished claims and invalidates evidence. Old owner requests fail. This does not stop old processes or fence external publication; confirmation must reflect actual stopped workers. Resume from observed state, restore required checks and reconfirm publication authority for the new session. For an unavailable/corrupt/incompatible registry, preserve it and stop mutations; never rebuild an empty registry over existing claims.

After confirmed resolution or deliberate abandonment, release each claim with `op: release`, `reason: merged` or `abandoned`, and `confirmed_stopped: true`. These are attestations by the supervisor, not automatic Git/GitHub checks. Preserve unfinished/dirty work and report it. Claims and action journals remain readable after release; pending action results can still be recorded by the owning generation.

## External action journal

Before an authorized mutation, write an intent:

```json
{
  "op":"intent","owner":"batch-example","generation":1,"issue":42,
  "id":"batch-example-42-pr-1","kind":"pr",
  "request":{"branch":"fix/42-example","head":"1111111111111111111111111111111111111111","base":"2222222222222222222222222222222222222222"}
}
```

Kinds `push` and `pr` require the recorded acceptance check to pass and `request.head`/`request.base` to match its verified revision; use `subissue` and `roadmap` for the other actions. The journal does not grant authority or execute a mutation. Reuse an ID only for the exact same intent. Record success with `op: result`, the same owner/generation/issue/id and a `result` object containing the actual remote identifiers. An already completed result is immutable except for an identical retry.

After interruption, an intent without a result means unknown outcome. Inspect the remote branch, PR list, created issue or work-unit body before retrying. If the mutation succeeded, record that result; if not, revalidate authority and prerequisites before executing. A changed head needs a new action ID and new evidence. Complete or preserve outstanding roadmap intents before releasing claims; never reconstruct publication permission from the journal.

## Work-unit completion

With current-session `--update-work-unit` permission to update the named container, acquire `roadmap:<N>` and inspect the live merged PR plus linked issue. Require a merged PR that completes the entire issue and a resolved issue state; closure alone, partial work, a green gate or CI, or an open PR is insufficient. Reconcile merges from other sessions with the same checks. If its body has no task checklist, skip this procedure: the native sub-issue list already shows task states. Never add one merely for this workflow.

Read the live body and retain its version/update timestamp. The offline helper (historical filename `roadmap.py`) prepares only checkbox substitutions:

```bash
uv run {{SKILLS}}/wu-coordinate-tasks/scripts/roadmap.py --body <private-body-file> --repository <owner/repo> --issues 42 43
```

It emits JSON containing `source_sha256` and the proposed `body`. It preserves other text and requires exactly one leading checkbox reference per issue (`#N`, an issue URL or a Markdown issue link). It ignores fenced examples, rejects missing/ambiguous references, and leaves unrelated dependency mentions unchanged. For another format, inspect and propose a minimal edit instead of replacing sections.

Journal the intended update with issue IDs and source digest. Immediately before writing, reread the body and compare it to the source; if it changed, rebuild the patch from the fresh body. Apply only the prepared body through the available authorized GitHub capability, then reread and verify the exact result. If using `gh`, also hold `github-cli` around account selection and each command.

The local resource protects cooperating sessions in this clone; it cannot prevent another machine or human from editing between remote read and write. If the service lacks conditional updates, do not claim atomic remote editing. When concurrent external edits are observed, stop automatic retry and reconcile with the user rather than repeatedly overwriting the body. After success, record the outcome and release the work-unit resource. If permission is missing, record the intended update as pending and report it without publishing.

## Existing registries

The rename keeps `<git-common-dir>/gitbox-coordination/registry.sqlite` and schema version 1. The internal `sessions.roadmap` column stores the work-unit number; `status` exposes `work_unit` alongside it. Historical `roadmap` action kinds and resource names remain readable so old journals can finish. New execution starts accept `work_unit`, not `roadmap`. A legacy value does not establish native membership: validate the explicit parent and every child before resuming, and preserve incompatible or unfinished work for review. Never erase or rebuild the live registry to adopt this workflow.
