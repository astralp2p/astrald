# fs-index-restart-reconcile

A persisted watch repository reconciles with its directory when node1
starts. Files edited, deleted, renamed, moved out, replaced, moved in, or
moved with their directory while node1 is stopped are reflected after the
restart in `objects.scan`, `objects.contains`, `objects.read` and
`objects.describe`, and the old ids and paths are gone.

- **roster** node1 · **after** `bootstrap-user-software-key` · **restart**
  node1 · **destroys** (it restarts node1 and leaves a persisted watch rooted
  under node1's root)
- **driver** `script.py`, every file through `lib/nodefs.py` under node1's
  root, so the directories lie on node1's machine in every world:
  1. Writes the watched directory W: `keep.txt`, `edit.txt` (96 B),
     `del.txt`, `mv-src.txt`, `moveout.txt`, `over.txt`, `sub/s1.txt` and
     `sub/s2.txt`, each with its mtime set an hour back. Writes the staging
     directory S, W's sibling: `over.new` (another length than `over.txt`,
     and a current mtime) and `new.txt`.
  2. Attaches W with a persisted `fs.new_watch` and waits until
     `objects.scan` lists exactly W's files.
  3. Records node1's restart count and the case map: each id W must hold
     after the restart (`present`) or must not (`absent`), with its path and
     the path a moved file left.
- **runner**: stops node1, runs `while_down.py`, starts node1 (see
  [Changes while nodes are down](../../README.md#changes-while-nodes-are-down)).
- **while_down** `while_down.py`, with node1 stopped, after checking that
  node1 does not answer:

  | case | change |
  |------|--------|
  | edit | overwrite `edit.txt` in place, same length |
  | del | delete `del.txt` |
  | moved | rename `mv-src.txt` to `moved.txt` |
  | moveout | move `moveout.txt` into S |
  | over | replace `over.txt` with `S/over.new` |
  | new | move `S/new.txt` into W |
  | sub | rename `sub/` to `sub2/` |
  | fresh | write `fresh/f1.txt` in a new directory |

- **oracle** `verify.py`:
  - node1's restart count is exactly one more than the driver saw;
  - the oracle walks W through `lib/nodefs.py`, hashes each regular file's
    bytes itself, and checks that set against the present cases and that no
    file is left at a path a case left;
  - within 30 s `objects.scan` lists exactly that set; on a timeout each
    case is reported with its state;
  - every present id is contained, read by repository returns its file's
    bytes, and `mod.fs.file_location` places it at its path on node1 and at
    no path it left;
  - every absent id (`edit.txt`'s and `over.txt`'s old bytes, `del.txt`,
    `moveout.txt`) is not contained, a read by repository is refused, and
    no file location names its old path;
  - `objects.repositories` lists the repository in the `local` group.

## Mechanism

- At start, `addTreeRepos` (`mod/fs/src/deps.go`) restores the watch, and
  `NewWatchRepository` adds its root to the indexer.
- The indexer's `init` (`mod/fs/src/indexer.go`) scans the widest roots: a
  path whose mtime matches its row is validated, any other path is
  invalidated. `InvalidatePathsCheckedOutside` then invalidates every row
  the scan did not see, and every invalidated path is queued.
- `checkAndFix` deletes the row of a path that no longer exists and hashes
  any other path again.

## Broken behaviour that turns it red

| broken behaviour | the oracle line that fails |
|------------------|----------------------------|
| the rows the scan did not see stay valid | the scan still lists `del` and `moveout` |
| the start-up scan is skipped | the scan lacks `moved`, `new`, `sub-s1`, `sub-s2` and `fresh` |
| a changed mtime still validates the row | the scan lacks `edit-new` and `over-new` and lists their old ids |
| the changes happen while node1 runs, or not at all | the disk walk, or `while_down.py` finding node1 up |

## Not asserted

A file replaced while node1 is down by one with the old mtime, whatever its
length or bytes. The start-up scan trusts `(path, mtime)`, a limit
`fs-repo-corrupt-served` takes up.
