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

5. **Reply to every comment** — after each fix is pushed, post a reply to the original thread referencing the commit. For stale or invalid comments, post a factual explanation of why no code change was made.

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

### 5 — Reply to every comment

For **valid comments** (after the fix is pushed), reply referencing the commit SHA:

```bash
gh api repos/cloudoperators/repo-guard/pulls/<PR>/comments \
  --method POST \
  --field in_reply_to=<comment-id> \
  --field body="Fixed in commit <sha>. <one-sentence summary of what changed.>"
```

For **stale or invalid comments**, reply explaining why no code change was made:

```bash
gh api repos/cloudoperators/repo-guard/pulls/<PR>/comments \
  --method POST \
  --field in_reply_to=<comment-id> \
  --field body="<factual explanation of why this is not actioned>"
```

Note: use `in_reply_to` (not a `/replies` sub-resource — that endpoint does not exist for PR review comments).

After replying, resolve the thread via the GraphQL API (there is no REST endpoint for this):

```bash
# Step 1 — get the thread node IDs
gh api graphql -f query='
{
  repository(owner: "cloudoperators", name: "repo-guard") {
    pullRequest(number: <PR>) {
      reviewThreads(first: 50) {
        nodes {
          id
          isResolved
          comments(first: 1) {
            nodes { databaseId body }
          }
        }
      }
    }
  }
}'

# Step 2 — resolve each thread by its node ID
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "<THREAD_NODE_ID>"}) {
    thread { id isResolved }
  }
}'
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
User:  Implement the valid ones, reply to all of them.
Agent: Edits api/v1/githuborganization_types.go, adds test file,
       runs make controller-test, commits (167822c), pushes.
       Posts reply to Comment 1: "Fixed in commit 167822c. All six dedup loops now..."
       Posts reply to Comment 2: "Fixed in commit 167822c. Added TestRepoChangeCalculator..."
       Posts reply to Comment 3: "This line no longer exists in the diff..."
       Resolves all three threads via GraphQL resolveReviewThread mutation.
```
