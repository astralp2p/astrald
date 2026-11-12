# fs-index-live

Files created, overwritten, deleted, or removed with their directory, in a
directory node1 watches, reach node1's index without a restart. `objects.scan`,
`objects.contains`, `objects.read` and `objects.describe` converge to exactly
the regular files on disk. A symlink is never indexed, and `fs.new_watch`
refuses bad paths and registers none of them.

- **roster** node1 · **after** `bootstrap-user-software-key` · `local_only`
  (it changes files in host directories under the session tree)
- **driver** `script.py`:
  1. Writes a baseline into W before the watch: `d0.txt` (40 B), `e0.txt`,
     `f0.txt`, `k.txt`, `sub/s1.txt`, `sub/s2.txt`, and a file OUT outside W.
  2. Attaches W with `fs.new_watch ... temporary=true`, waits until node1
     holds every baseline id, then sleeps 2.1 s.
  3. Changes W while node1 runs:

     | case | change |
     |------|--------|
     | a1 | create `a1.txt` empty, sleep 1.5 s, write it |
     | a2 | write `a2.txt` in one go (Linux only) |
     | a3 | `mkdir newdir`, sleep 1.5 s, write `newdir/n1.txt` |
     | d | overwrite `d0.txt` in place, same length |
     | e | rewrite `e0.txt`, different length |
     | f | unlink `f0.txt` |
     | r | `rm -r sub` |
     | g | symlink `link` to OUT |

  4. Calls `fs.new_watch` on a relative path, a missing directory and a
     regular file, and records the answers.
- **oracle** `verify.py`:
  - walks W itself, hashes the regular files, and checks that set against the
    facts' present cases;
  - polls up to 20 s until `objects.scan` lists exactly that set; on a timeout
    it prints every case as `ok`, `missing` or `stale` and fails once;
  - each present file is contained, read by repository returns its bytes, and
    `mod.fs.file_location` places it at its path on node1;
  - each absent id (old d, old e, f0, s1, s2, and OUT behind the symlink) is
    not contained, a read by repository is refused, and no file location
    names its old path;
  - four seconds later the scan still lists exactly the set;
  - the three bad paths are refused again, and `objects.repositories` lists
    none of them.

## Mechanism

- `NewWatchRepository` (`mod/fs/src/watch_repository.go`) wires
  `OnWriteDone` and `OnRenamed` to a requeue, `OnRemoved` to a row delete,
  and `OnDirCreated` to a new watch. `OnFileCreated` stays nil.
- A Write event arms a 3 s debounce (`DefaultWriteTimeout`,
  `mod/fs/src/watcher.go`); its end requeues the path, and the indexer
  rehashes it when its mtime changed (`checkAndFix`, `mod/fs/src/indexer.go`).
- A Remove event deletes the one row of its path. `rm -r` reports each file
  from the watch on its directory.
- A Create event of a regular file does nothing while `OnFileCreated` is nil,
  so the symlink, whose `os.Stat` follows to OUT, is never indexed.
- `validPath` (`mod/fs/src/valid_path.go`) refuses a relative, missing or
  non-directory path in band, before anything registers.

## Platforms

Linux inotify is authoritative. On macOS kqueue a file created and written in
one go can report only its Create before the per-file watch exists, and a
Create does nothing here. So a1 and a3 create first and write 1.5 s later,
and the driver skips a2 off Linux. The oracle prints the platform.

## Out of scope

A file renamed or moved into W, a rename within W, a rename over an indexed
file, and a directory rename belong to `fs-index-live-rename`. A same-length
rewrite that keeps its mtime belongs to `fs-repo-corrupt-served`.

## Broken behaviour that turns it red

| broken behaviour | the cases that fail |
|------------------|---------------------|
| `OnWriteDone` not wired | a1, a2, a3, d, e missing; old d and e stale |
| `OnRemoved` not wired | f, r-s1, r-s2 stale |
| `OnFileCreated` wired (the rename fix) with no symlink check: the link's Create arms the debounce and `os.Stat` follows it | g stale |

The watch is temporary, so no tree entry outlives a restart of the shared
world. The directories stay in the results tree after the run.
