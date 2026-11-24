# Kitwork Engine

Kitwork runs an application from a folder, and is built so that the host stays up when the code in
that folder was written by someone else, or by an AI.

This repository holds the engines of Kitwork: the two written before, kept to be read, and the one
being written again.

## What is where

| Path | What it is |
| :--- | :--- |
| `archive/engine/` | The first engine, Go module `github.com/kitwork/engine`, tags `v0.1.0` to `v0.1.4`. It is kept to be read and is no longer developed. Its own README, documentation and licence files are in that folder, and it builds from there. |
| `archive/engine2/` | The second engine, Go module `github.com/kitwork/engine2`, as one commit without its history. Its documentation is in `archive/engine2/docs/`. |
| the root | Reserved for the engine written again. It is not here yet. |

The move into `archive/engine/` was a rename of every file and nothing else: the tree of that folder
is the tree the root had one commit before, and the history of each file follows it
(`git log --follow`).

`archive/engine2/` is the tree of that engine on 24 November 2026, byte for byte, but for one line:
its `go.mod` replaces `github.com/huynhnhanquoc/sqlitewire` with a folder beside this repository
(`../../../sqlitewire`), so it builds only with
[sqlitewire](https://github.com/huynhnhanquoc/sqlitewire) checked out there.

This repository was `github.com/kitwork/engine` until 24 November 2026. The old address redirects
here.

## Licence

GNU Affero General Public License, version 3: see `LICENSE`. The files of the first engine carry
their own copy, and its drafts of an application exception and a contributor agreement, in
`archive/engine/`.

Files that came from elsewhere keep their own licences, written beside them. In `archive/engine2/`
those are the fonts (`library/design/families/FONTS_LICENSE`), the Tabler icons
(`library/ui/icons/TABLER_LICENSE`) and the gradients (`library/ui/stylesheet/WEBGRADIENTS_LICENSE`).
