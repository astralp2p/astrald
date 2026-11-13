# indexing-fs-watch-feed

A watched directory feeds a subscribed indexer through real fsnotify events.
Each Index names the hash of the file's bytes, a second file with the same
bytes adds no Index, and after a restart the indexer's changelog folds to the
hashes of the files on disk, with an Unindex only for an id whose last holder
is gone.

- **roster** node1 · **after** `bootstrap-user-software-key` · **restart**
  node1 · **destroys** (it restarts node1 and leaves a persisted watch and an
  enabled index)
- **driver** `script.py`, as the User and one app I with an unconstrained
  `mod.auth.serve_objects_action` grant, every file of W written through
  `lib/nodefs.py` under node1's root, so W lies on node1's machine in every
  world:
  1. writes `seed.txt` (c1) into W, attaches W with `fs.new_watch`
     (persisted), and waits until the repository contains c1;
  2. I registers an indexer, the User enables the repository with
     `indexing.enable_repo`, and I subscribes: Index c1;
  3. writes `f2.txt` = c2: Index c2. Writes `f3.txt` = c2 and waits until
     `objects.describe` places c2 at `f3.txt`;
  4. writes `f4.txt` = c4: Index c4. Rewrites `f4.txt` in place, truncating,
     with c4p: Index c4p;
  5. deletes `f2.txt`, whose c2 `f3.txt` still holds, then `seed.txt`.

  I acks every change. After each step the driver reads for four more seconds
  and records what arrives in `live_tail`; nothing asserts on it.
- **runner**: restarts node1.
- **oracle** `verify.py`: subscribes as I with the token the driver recorded
  and acks every change, then:
  - before the restart I received Index c1, c2, c4, c4p in write order and no
    other Index;
  - the first change after the restart is at the version after the last ack,
    and the whole changelog has consecutive versions;
  - the changelog indexes each of c1, c2, c4, c4p once and unindexes exactly
    c1 and c4, live or after the restart;
  - the fold of the changelog equals the hashes of the files under W, which
    equal the ids `objects.scan` lists;
  - `objects.repositories` lists the repository, and a second subscription
    is silent.

All ids are hashed in the oracle from the payloads and the files on disk,
whose bytes the oracle reads through `lib/nodefs.py`.

## Mechanism

- A write ends in the 3 s debounce (`mod/fs/src/watcher.go`), the indexer
  hashes the file and pushes an `IndexEvent` (`checkAndFix`,
  `mod/fs/src/indexer.go`), and a following `WatchRepository.Scan` passes it
  on (`mod/fs/src/watch_repository.go`).
- `syncRepo` (`mod/indexing/src/repos.go`) adds each followed id with
  `addToRepo`, which writes no change for an id already present
  (`mod/indexing/src/db.go`). A duplicate file is therefore silent.
- A Remove event deletes one row and pushes no event, and a following scan
  reports added objects only. The restart's resync compares a fresh snapshot,
  the distinct ids under the root (`UniqueObjectIDs`, `mod/fs/src/db.go`),
  with the index and writes an Unindex for each id that left. c2 stays in the
  snapshot while `f3.txt` holds it.

## The Unindex of a delete comes from the restart

A live delete writes no Unindex on master, which `indexer-cursor` also
records. The oracle judges the whole changelog, so it holds whether the
Unindex arrives live or after the restart.

## Platforms

Every new file is created empty and written 0.5 s later: on macOS kqueue a
file created and written in one go can report only its Create before the
per-file watch exists, and a Create indexes nothing. The rewrite of `f4.txt`
truncates in place and never renames, so it is a Write on both.

## Broken behaviour that turns it red

| broken behaviour | the oracle line that fails |
|------------------|----------------------------|
| `addToRepo` adds an id already present | Index c2 twice before the restart |
| a row delete removes every row with the same id | c2 unindexed and indexed again |
| the resync skips the ids that left | no Unindex of c1 and c4 |
| `OnWriteDone` not wired | Index c2, c4, c4p missing before the restart |
| the index upsert keeps the old `data_id` | the fold differs from the disk |
| the follow phase adds nothing | Index c2, c4, c4p missing before the restart |
