# Node

`core.Node` owns the `Modules` manager and node assets and runs the module
lifecycle below.

Encrypted links to peers are provided by `mod/nodes`, not the core node.

## Module Lifecycle

Load -> Inject -> LoadDependencies -> Prepare -> Run

* Load: instantiate the module. Do not access other modules.
* Inject: `core` auto-registers each module that implements `astral.Router` or
  `QueryPreprocessor` with the node.
* LoadDependencies: fill Deps structs with `core.Inject`; register resolvers
  and filters.
* Prepare: apply pre-run config. All dependencies are available.
* Run: block the goroutine until context cancellation.

## Scheduler

`mod/scheduler` runs tasks only after all declared dependencies signal Done. It
is a module, not part of `core.Node`.
