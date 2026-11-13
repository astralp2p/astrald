# repo-removal-sticks

A repository removed with `objects.remove_repository` stays removed after
node1 restarts. A temporary repository ends with the restart. A persisted
repository is restored and serves its object. No removal deletes a file.

- **after** `bootstrap-user-software-key` · **restart** node1 · **destroys**
  true (it restarts node1 and leaves a persisted repository rooted under
  node1's root) · **roster** node1
- **driver** `script.py`: as the User, attach three directories under
  node1's root, made through `lib/nodefs.py` so they lie on node1's machine
  in every world, with `fs.new_repo`:
  - `repo-removal-sticks-kept`, persisted;
  - `repo-removal-sticks-temp`, with `temporary=true`;
  - `repo-removal-sticks-gone`, persisted.

  Store a run-unique payload in each with `objects.create`, check that each
  file is on disk and served, then remove `gone`. The driver checks that
  `gone` is unlisted, out of the `local` group and unserved right after the
  removal, and records node1's restart count from its session view.
- **runner**: restarts node1.
- **oracle** `verify.py`:
  - node1's restart count is exactly one more than the driver saw;
  - `objects.repositories` lists `kept` and lists neither `gone` nor `temp`;
    the `local` group names `kept` and neither of the other two;
  - `kept` serves its exact bytes, by name and through the default read
    repository;
  - a load of `gone`'s or `temp`'s object by repository name answers the
    in-band `error_message` "repository not found", and a load through the
    default read repository answers an `error_message` too;
  - all three files are still on disk with their bytes, read through
    `lib/nodefs.py`.

## Mechanism

- `fs.new_repo` (`mod/fs/src/op_new_repo.go`) saves a tree entry under
  `/mod/fs/repos/<name>` unless `temporary` is set.
- At start, `addTreeRepos` (`mod/fs/src/deps.go`) registers every tree entry
  and adds it to `local`.
- `objects.remove_repository` (`mod/objects/src/op_remove_repository.go`)
  drops the repository and calls its `AfterRemoved`, which deletes the tree
  entry (`deletePersisted` in `mod/fs/src/repos_tree.go`). A failed delete
  is logged and not reported to the caller, so the entry restores the
  repository at the next start. Only a restart shows that failure.

## Broken behaviour that turns it red

| broken behaviour | the oracle line that fails |
|------------------|----------------------------|
| the tree delete on removal fails or is skipped | `gone` is listed after the restart |
| a temporary repository is saved to the tree | `temp` is listed after the restart |
| the tree entries are not restored at start | `kept` is not listed after the restart |
| a removal deletes the repository's directory | `gone`'s file is gone from disk |

## Scope

The repositories are writable fs repositories (`fs.new_repo`).
`fs.new_watch` repositories share `deletePersisted` but are not attached
here. The directories stay under node1's root after the run: in the results
tree in a local world, in node1's container in a Docker world.
