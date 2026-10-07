# Decision: one git worktree per agent

## The incident

On 2026-10-07 a commit labelled `docs(memory): record the dual deployment paths
and tag verification` (`feea4c1`) also contained eight files of another agent's
uncommitted work:

```
server/internal/app/app.go
server/internal/store/aggregation.go
server/internal/store/aggregation_test.go
server/internal/store/store.go
web/src/components/SystemMetrics.svelte
web/src/components/screens/DeviceOverview.svelte
web/src/lib/telemetry-store.test.ts
web/src/lib/telemetry-store.ts
```

The cause was `git add -A` run without first checking `git status`. Two agents
were editing the same working directory, so the index is shared and `-A` stages
everything modified, not just the author's own files.

The commit was pushed, which turned `origin/develop` red: the frozen snapshot
sat mid-edit and failed lint on `app.go` (a `gofmt` error and a `shadow`
declaration). No work was lost, but it was attributed to the wrong commit and
left the branch failing until the other agent finished.

`origin/main` was unaffected: the merge from the previous commit had already
happened, so `v0.4.0` and its image are clean.

## What made it possible

Not carelessness alone. The environment invites it:

- two agents, one working directory, one git index
- `git add -A` and `git commit -a` are the path of least resistance
- the failure is silent: `git status` shows files, but a busy tree makes it easy
  to skim and assume they are all yours

The signal that was missed: this was the only commit in the session where
`git status` was not run before staging. Every other commit checked first and was
clean. One skipped check, one contaminated commit.

## The decision

**Each agent gets its own `git worktree`.** This removes the shared index
entirely rather than relying on discipline.

```bash
git worktree add ../sync-win-<topic> -b <branch>
```

A worktree is a second checkout of the same repository with its own working
directory and its own index, sharing the object database and refs. Commits from
one are visible to the other immediately; no clone, no fetch, no push.

## Rules that follow

1. **Never `git add -A` or `git commit -a` in a shared directory.** Stage
   explicit paths. In a dedicated worktree, `-A` is safe because every change
   belongs to that worktree's work.
2. Run `git status --short` before staging and read the list. If a path is not
   yours, stop.
3. One worktree per concurrent task, not per agent identity. Two tasks by the
   same agent in parallel have the same problem.
4. Rebase onto the shared branch rather than merging across worktrees, to keep
   the history linear.
5. Remove the worktree when the task lands: `git worktree remove <path>`.

## Mechanics worth knowing

- `git worktree list` shows every checkout and the commit each is on.
- Each worktree has its own index and `HEAD`, but they share refs, so a branch
  cannot be checked out in two worktrees at once.
- `node_modules`, build caches and `data-dev` are per-worktree, so a new
  worktree needs its own install. That is the cost, and it is the price of
  isolation.
- Gitignored files are not copied. A worktree that needs `.env` must have it
  created there.

## Why this beats discipline

The incident was not a knowledge gap: the correct behaviour was already known and
followed in every other commit. It failed once, under load, in the way that
single mistakes fail. The worktree removes the shared resource, so the mistake
becomes impossible rather than merely discouraged.
