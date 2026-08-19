# gh-stack-merge

Merge each pull request in a GitHub stack one at a time, from bottom to top. Use this as a workaround when the GitHub UI cannot merge the whole stack. GitHub recommends merging one pull request at a time while it fixes the underlying stack merge issues. See [GitHub's stacked pull request discussion](https://github.com/github/gh-stack/discussions/212).

The command waits for required checks and GitHub's merge queue before it continues.

## Install

Install [GitHub CLI](https://cli.github.com/) and the [`github/gh-stack`](https://github.com/github/gh-stack) extension first. `gh-stack-merge` supports `gh-stack` 0.1.x.

```sh
gh extension install github/gh-stack
gh extension install tekumara/gh-stack-merge
```

## Use

Run the command from the local repository that contains your stack. It only operates on the current stack in the current directory. Check out any branch in that stack, then run:

```sh
gh stack-merge
```

The command uses squash merge by default. Choose another merge method with one of these flags:

```sh
gh stack-merge --merge
gh stack-merge --rebase
gh stack-merge --squash
```

Example run:

```console
$ gh stack-merge --squash

PR #293: checking required checks
PR #293: merging
PR #293: merged
PR #294: checking required checks
PR #294: merging
PR #294: waiting for required workflows
PR #294: merged
PR #295: checking required checks
PR #295: merging
PR #295: waiting for required workflows
PR #295: merged
PR #296: checking required checks
PR #296: merging
PR #296: waiting for required workflows
```

The command snapshots the current stack when it starts. It skips merged pull requests and resumes queued ones. It stops if checks fail, GitHub rejects a merge, or a pull request closes without merging.

Press Ctrl-C to stop waiting. This does not cancel work already queued on GitHub. Run the command again to resume from the first unmerged pull request.

## Develop

Run the tests with:

```sh
go test ./...
```

## Licence

[MIT](LICENSE)
