# review-pr

Review an open pull request, assess every comment for validity, implement valid fixes (with approval), and reply to invalid ones.

## Usage

```
/review-pr <PR-number-or-URL>
```

## What this command does

1. **Fetch comments** — retrieve all inline review comments and conversation comments from the PR using `gh api`.

2. **Assess each comment** — for every comment decide:
   - **Valid**: the comment identifies a real bug, correctness issue, or meaningful improvement in the code *as it exists in the PR diff*.
   - **Stale/inapplicable**: the comment refers to a line that no longer exists or a condition that the PR has already addressed.
   - **Invalid/disagree**: the suggested change would be incorrect, harmful, or violates project conventions.

3. **Present findings** — explain each comment and your assessment to the user before touching any code. Get explicit approval to proceed.

4. **Implement valid fixes** — for approved valid comments:
   - Read the relevant files first.
   - Apply the minimal change that addresses the comment.
   - Run `make controller-test` to verify nothing is broken.
   - Commit with a DCO sign-off (`git commit -s`) following the `type(scope): description` style from `AGENTS.md`.
   - Push to the PR branch.

5. **Reply to stale or invalid comments** — for comments that should not be acted on, compose a short, factual reply explaining why and (if the platform supports it) resolve the thread. Get user approval before posting.

## Steps in detail

### 1 — Gather PR data

Run all three commands in parallel:

```bash
gh pr view <PR> --repo cloudoperators/repo-guard --json title,body,comments,reviews,url
gh api repos/cloudoperators/repo-guard/pulls/<PR>/reviews
gh api repos/cloudoperators/repo-guard/pulls/<PR>/comments
```

Also read the current branch to understand what the PR has already changed:

```bash
git diff main...HEAD -- <changed-files>
```

### 2 — Assess comments

For each comment:
- Quote the comment body.
- Show the diff hunk it points at.
- State: **Valid** / **Stale** / **Invalid** with a one-paragraph rationale.

### 3 — Get approval

Present the full assessment and ask the user whether to proceed before making any change.

### 4 — Implement fixes

For each approved valid fix:

1. Read the full file (`Read` tool).
2. Apply the change (`Edit` tool — prefer targeted edits over full rewrites).
3. Run tests: `go test ./api/v1/... -v` then `make controller-test`.
4. If tests pass, commit and push:

```bash
git add <files>
git commit -s -m "fix(scope): <description>"
git push origin <branch>
```

### 5 — Reply to non-actionable comments

Post a concise reply via the GitHub API:

```bash
gh api repos/cloudoperators/repo-guard/pulls/comments/<comment-id>/replies \
  --method POST \
  --field body="<reply text>"
```

## Conventions (from AGENTS.md)

- All commits must be signed off (`-s`).
- Commit style: `type(scope): short description` — e.g. `fix(types): normalize team slugs`.
- Do **not** add `Co-authored-by` trailers.
- Run `make fmt` before committing if you touched Go files.
- No CRD changes needed unless `_types.go` struct fields changed — if they did, run `make manifests generate`.
- After any fix, run `make controller-test` and confirm all specs pass.

## Example session

```
User:  /review-pr 113
Agent: Fetches comments from PR #113.
       Presents: Comment 1 (Valid) — normalize op.Team in dedup checks ...
                 Comment 2 (Valid) — add unit tests ...
                 Comment 3 (Stale) — wrong constant reference, but the line no longer exists ...
       Asks for approval.
User:  Implement the valid ones, reply to the stale one.
Agent: Edits api/v1/githuborganization_types.go, adds test file,
       runs make controller-test, commits, pushes.
       Posts reply to Comment 3 explaining it is stale.
```
