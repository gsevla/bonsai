# bonsai

A terminal tool that scans a directory for git repositories, lists every worktree it finds (repository, branch, disk usage, last change, status) and lets you select the ones to remove.

## Requirements

- macOS or Linux
- Git 2.41 or newer on your `PATH` (merge detection uses the `%(ahead-behind:...)` atom of `git for-each-ref`)
- Go 1.27 or newer, only to build from source. The resulting binary has no runtime dependency besides Git.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/gsevla/bonsai/main/install.sh | sh
```

This downloads the latest release for your OS and architecture, verifies its checksum and puts `bonsai` in `~/.local/bin`, which must be on your `PATH`. Set `BONSAI_INSTALL_DIR` to install somewhere else.

### From source

```bash
go install .
```

This puts `bonsai` in `$(go env GOPATH)/bin`, which must be on your `PATH`. To build a local binary instead:

```bash
go build -o bonsai .
```

If you use [mise](https://mise.jdx.dev), `mise install` in this directory provides the pinned Go toolchain.

## Usage

```
bonsai [OPTIONS] [PATH]
```

`PATH` defaults to the current directory. Options go before `PATH`.

| Flag | Default | Description |
| --- | --- | --- |
| `-p`, `--path <DIR>` | `.` | Root directory to scan |
| `-d`, `--depth <N>` | `5` | Maximum directory depth when looking for repositories |
| `-a`, `--min-age <DAYS>` | `0` | Only show worktrees unchanged for at least this many days |
| `-s`, `--min-size <SIZE>` | `0` | Only show worktrees at least this large (`500M`, `2G`) |
| `-n`, `--dry-run` | | Go through the removal flow without removing anything |
| `-f`, `--force` | | Do not require typing `DELETE` to remove dirty worktrees |
| `--no-fetch` | | Do not fetch remotes before scanning. Status may be stale |
| `--plain` | | Print a plain table instead of opening the TUI |
| `--json` | | Print worktree metadata as JSON |
| `-v`, `--version` | | Print the version |

### Keys

| Key | Action |
| --- | --- |
| `↑`/`k`, `↓`/`j`, `PgUp`, `PgDn`, `g`, `G` | Move |
| `space` | Toggle the focused worktree |
| `a` | Select or clear all visible worktrees |
| `m` | Select every merged worktree without uncommitted changes |
| `/` | Filter by repository or branch name |
| `o` | Cycle sort order: repo, size, modified |
| `d` | Remove the selected worktrees (asks for confirmation) |
| `q`, `esc` | Quit |

## Status column

| Status | Meaning |
| --- | --- |
| `protected` | Main worktree, bare repository, or the worktree of the base branch. Cannot be selected. |
| `locked` | Locked with `git worktree lock`. Cannot be selected. |
| `merged` | The branch has no commits missing from the base branch |
| `gone` | The upstream branch was deleted on the remote, which usually means its pull request was merged |
| `N ahead` | The branch has N commits the base branch does not have |
| `missing` | Git still tracks the worktree but its directory no longer exists |
| `dirty` | Uncommitted changes or untracked files |

The base branch is `origin/HEAD` when it is set, otherwise `main` or `master`. A branch is compared with both the remote and the local copy of the base. Squash and rebase merges rewrite commits, so they show up as `N ahead` until the remote branch is deleted and fetched with `git fetch --prune`, at which point they become `gone`.

## Fetching

Merged, `gone` and `N ahead` are computed from the local copy of the remote branches, which is only as fresh as your last fetch. To avoid showing stale or wrong status, bonsai runs `git fetch --all --prune` on every repository before it scans (once per repository, 60 second timeout, never prompting for credentials). Repositories without remotes are skipped.

If a fetch fails (offline, expired credentials, timeout), bonsai still lists the worktrees, but warns that status may be stale: a yellow note in the TUI header, a message on stderr for `--plain` and `--json`, and `"stale": true` on the affected entries in JSON output.

Pass `--no-fetch` to skip fetching, for offline use or very large repositories. Status then reflects your last manual fetch.

## How it works

- Discovery walks the tree up to `--depth`, stops at the first directory that is a repository (a `.git` entry or a bare repository), and asks `git worktree list --porcelain` for its worktrees. Worktrees living outside the scanned directory are listed too.
- Disk usage is counted in allocated blocks, like `du`, with hard links counted once.
- Modified is the newest of the HEAD commit date and the modification time of any file outside `.git`.
- Removal runs `git worktree remove`, adding `--force` only for worktrees you confirmed as dirty. Branches are never deleted.

## Development

```bash
go test ./...
```

Pushing a `v*` tag runs GoReleaser in GitHub Actions and publishes the binaries that `install.sh` downloads.
