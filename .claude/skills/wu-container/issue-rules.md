# Work-unit issue rules

Read this from `wu-container`, `wu-tasks` or `wu-coordinate-tasks`. I use one model and one set of GitHub mechanics across both harnesses.

## Model and validation

- An **independent issue** has no parent, no children and no `work-unit` label. I implement it through `work-issue`.
- A **work unit** has the `work-unit` label, no parent, a complete purpose/problem/scope/HLR/AC body and direct task children. A new container can be empty until `wu-tasks` decomposes it.
- A **task** has exactly that work unit as its native GitHub parent, belongs to the same repository, has no children and does not carry `work-unit`. Its body names `Parent: #N` and `Advances: AC1, AC2` for human readers; those lines do not establish membership.

Validate issue kind (not a PR), repository, state, labels, parent and the complete paginated child list. Require an open parent for creation or new execution. Closed tasks remain useful evidence; closure alone does not prove their criteria passed. Never infer membership from titles, references, roadmap checkboxes or search matches. A task with children, a nested container or a different parent is invalid for this model.

`work-unit` is the canonical marker, independent of optional GitHub issue types. Reuse the label if present. If absent, include creation of `work-unit` (description `Container for a unit of work and its task sub-issues`, color `5319e7`) in the concrete publication proposal. Never silently relabel a legacy roadmap or link all issues mentioned in its body. Migration needs a reviewed scope, requirements, criteria and exact child set, with authorization for those changes.

## Access and native relationships

Derive the repository from `remote.origin.url`. Prefer connected GitHub tools when they expose the required data and mutations. If native parent/children/order operations are missing, use `gh api` following `.claude/CLAUDE.md`'s account selection rule before each command. Hold the coordination `github-cli` resource when working in a registered batch, including account selection and the complete operation. Do not change credentials or import account names, type IDs, projects or milestones from another repository.

Use the [GitHub sub-issues REST API](https://docs.github.com/en/rest/issues/sub-issues) when the connector lacks these operations:

- `GET /repos/{owner}/{repo}/issues/{number}` reads the issue, including its numeric `id`.
- `GET /repos/{owner}/{repo}/issues/{number}/parent` reads the native parent.
- `GET /repos/{owner}/{repo}/issues/{number}/sub_issues?per_page=100` lists children; follow every pagination link and preserve returned order.
- `POST /repos/{owner}/{repo}/issues/{parent}/sub_issues` links the task with JSON `{"sub_issue_id": <task database id>, "replace_parent": false}`.
- `PATCH /repos/{owner}/{repo}/issues/{parent}/sub_issues/priority` reorders a child using `sub_issue_id` and either `after_id` or `before_id`, all numeric issue database IDs, not issue numbers or GraphQL node IDs.

Do not treat authentication, permission or network failures as an empty list. Interpret a parent endpoint's 404 as no parent only after confirming access to the issue and repository and that the endpoint is supported; ambiguous access failures block validation. Prefer a supported API that explicitly returns a null parent when available. Do not replace a failed native-link operation with a body reference.

## Drafts, authority and recovery

Keep unpublished drafts and a small operation journal under `<git-common-dir>/gitbox-coordination/`, never in tracked documentation. Record the repository, parent if any, proposed titles/bodies/metadata, approved actions and returned issue IDs/URLs. Coordinate batches use their own protocol journal. Local draft files confer no publication authority.

Prepare the concrete result before asking for missing publication authorization. For task creation, describe creation, native linking, metadata inheritance and any ordering changes as one scoped proposal. Existing current-session authority can cover those named actions; do not ask again. No creation skill authorizes implementation, PRs, merges, closing a container or changing an unrelated issue.

Use structured arguments for bodies, or UTF-8 files passed to `gh issue create --body-file`; never construct shell commands from untrusted prose. Use `#N` only for real issue references; requirement IDs are `HLR1` and `AC1`, never `#1`. Apply existing bug/enhancement/priority labels only when justified. Inherit task assignees and milestone from the parent unless the user specifies otherwise; absent values remain absent. Projects and custom types are optional, not prerequisites.

Journal intent before each external mutation and its identifiers immediately after success. If a call times out, inspect the remote and resolve whether it succeeded before retrying. After creation, read back title, body and metadata. After linking, check both the parent list and the child's parent. After ordering, read the complete list and compare it to the agreed order. Never report success solely from a mutation response.

If creation succeeds but linking or ordering fails, retain the created issue and its ID, report the partial state and resume at the failed step. Do not create duplicates or delete partially completed work. If a task already belongs to a different parent, stop that operation; never use `replace_parent: true` without a separate explicit reparenting request. Recheck the full child set before reordering; if it changed, incorporate the new evidence and review the order instead of overwriting concurrent work.
