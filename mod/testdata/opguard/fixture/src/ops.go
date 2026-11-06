// Package fixture holds one op of each kind for TestFindUnguardedOps.
package fixture

type Module struct{ Auth auth }

type auth struct{}

func (auth) Authorize(any) bool { return true }

type other struct{}

// OpDirect calls Authorize itself.
func (mod *Module) OpDirect() { mod.Auth.Authorize(nil) }

// OpViaHelper calls Authorize through a helper of the same package.
func (mod *Module) OpViaHelper() { mod.admit() }

func (mod *Module) admit() { mod.check() }

func (mod *Module) check() { mod.Auth.Authorize(nil) }

// OpWhyInDoc needs no action.
//
// why: the reason sits in the doc comment.
func (mod *Module) OpWhyInDoc() {}

// OpWhyInBody needs no action.
func (mod *Module) OpWhyInBody() {
	// why: the reason sits in the body.
}

// OpNotOnModule is not an op: its receiver is not *Module.
func (o *other) OpNotOnModule() {}

// Opaque is not an op: "aque" is not exported.
func (mod *Module) Opaque() {}

// OpUnguarded has no guard. Its reason uses an old wording that is not the tag.
//
// why no action check: this wording does not count.
func (mod *Module) OpUnguarded() { mod.loop() }

func (mod *Module) loop() { mod.loop() }
