# Contributing

Enable the repository's git hooks once per clone:

```shell
git config core.hooksPath .githooks
```

The setting applies to every worktree of the clone. The pre-commit hook runs `scripts/check-astral-blueprints.sh`, which checks that every Go type with an `ObjectType()` method is registered through `astral.Add`.
