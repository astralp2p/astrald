# mod/scheduler

Runs in-process tasks once their declared dependencies have completed. Owns the
in-memory scheduled-task queue, task cancellation and completion handles,
dependency release hooks, and event fan-out to running tasks.

## Dependencies

| Module | Why |
|---|---|
| `objects` | the module implements `objects.Receiver`; object auto-registration lets it receive event drops |
| `events` | `ReceiveObject` forwards only `*events.Event` values to running tasks that implement `EventReceiver` |
| `core` | module registration and dependency injection for the scheduler module |

## Dependency Gate

* A task runs only after every dependency signals `Done`, unless the scheduled task is canceled first.
* `prepare` accepts only `StateScheduled`; other states return `ErrInvalidState`.
* `ScheduledTask.done` closes exactly once via `doneOnce`.
* Releasers collected from completed dependencies run after the task runner exits.
* Events are delivered only to tasks in `StateRunning`.
* `Schedule` returns `ErrNotRunning` before `Run` stores a context and after that context is done.
* `LockPool` returns a `Done` dependency whose first `Done()` locks the requested pool items and whose `Release` unlocks them once.
