# Kitwork Engine

Kitwork runs an application from a folder, and is built so that the host stays up when the code in
that folder was written by someone else, or by an AI.

This repository holds the engines of Kitwork, the one that was written first and the one being
written again.

## What is where

| Path | What it is |
| :--- | :--- |
| `archive/engine/` | The first engine, Go module `github.com/kitwork/engine`, tags `v0.1.0` to `v0.1.4`. It is kept to be read and is no longer developed. Its own README, documentation and licence files are in that folder, and it builds from there. |
| the root | Reserved for the engine written again. It is not here yet. |

The move into `archive/engine/` was a rename of every file and nothing else: the tree of that folder
is the tree the root had one commit before, and the history of each file follows it
(`git log --follow`).

This repository was `github.com/kitwork/engine` until 24 November 2026. The old address redirects
here.

## Licence

GNU Affero General Public License, version 3: see `LICENSE`. The files of the first engine carry
their own copy, and its drafts of an application exception and a contributor agreement, in
`archive/engine/`.
