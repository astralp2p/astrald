# fs-watch-restart-restore

A watch repository follows the persistence rules across a restart of node1.
A persisted `fs.new_watch` repository is restored, indexed and serves its
file. A temporary one ends with the restart. A removed one stays removed,
and its file is no longer described. No removal or restart deletes a file.

- **roster** node1 · **after** `bootstrap-user-software-key` · **restart**
  node1 · **destroys** (it restarts node1 and leaves a persisted watch rooted
  in the results tree) · `local_only` (it attaches host directories and
  restarts node1)
- **driver** `script.py`: as the User, writes one run-unique file into each of
  three sibling directories, then watches each with `fs.new_watch`:
  - `fs-watch-restart-kept`, persisted;
  - `fs-watch-restart-temp`, with `temporary=true`;
  - `fs-watch-restart-gone`, persisted.

  The driver waits until each repository contains its file's id and
  `objects.describe` places it at its path, then removes `gone` with
  `objects.remove_repository`. It records what node1 answers right after the
  removal and node1's restart count.
- **runner**: restarts node1.
- **oracle** `verify.py`:
  - right after the removal, `gone` was unlisted, out of `local`, not
    contained in `local`, unread through the default read and described at
    no path;
  - node1's restart count is exactly one more than the driver saw;
  - `objects.repositories` lists `kept` and neither `temp` nor `gone`; the
    `local` group names `kept` alone of the three;
  - within 20 s `kept` contains its id, serves its bytes by name, and
    `mod.fs.file_location` places it at its path on node1;
  - a load of `temp`'s or `gone`'s id by name answers "repository not found",
    and a read through the default read is refused;
  - within 20 s no file location describes `temp`'s or `gone`'s id;
  - all three files are still on disk with their bytes.

## Mechanism

- `fs.new_watch` (`mod/fs/src/op_new_watch.go`) saves a tree entry under
  `/mod/fs/repos/<name>` unless `temporary` is set.
- At start, `addTreeRepos` (`mod/fs/src/deps.go`) builds a
  `WatchRepository` for each entry that is not writable. The indexer's
  `init` (`mod/fs/src/indexer.go`) rescans the registered roots and
  invalidates every row it did not check, so rows under `temp` and `gone`
  stop being described.
- A watch repository's `AfterRemoved` (`mod/fs/src/watch_repository.go`)
  removes its root from the indexer, which soft-deletes the rows under it,
  and deletes its tree entry.

## The directories are siblings

`kept`, `temp` and `gone` sit side by side under the test's own results
directory. No root is a parent of another: `removeRoot` keeps the rows of a
root another root covers, and `gone` would then stay described after its
removal.

## Broken behaviour that turns it red

| broken behaviour | the oracle line that fails |
|------------------|----------------------------|
| the tree entries are not restored at start | `kept` is not listed |
| a temporary watch is saved to the tree | `temp` is listed |
| the tree delete on removal is skipped | `gone` is listed |
| the indexer keeps the removed root's rows | `gone` is described right after its removal |

## Scope

`repo-removal-sticks` covers the same rules for writable `fs.new_repo`
repositories. Content changed while node1 is stopped belongs to
`fs-index-restart-reconcile`.
