package vfs

import (
	"errors"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

type authFixture struct {
	mod    *Module
	auth   *fakeAuth
	vfs    *astral.Identity
	user   *astral.Identity
	caller *astral.Identity
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	f := &authFixture{
		mod:    testModule(t),
		auth:   newFakeAuth(),
		vfs:    astral.GenerateIdentity(),
		user:   astral.GenerateIdentity(),
		caller: astral.GenerateIdentity(),
	}
	f.mod.Auth = f.auth
	f.mod.User = fakeUser{id: f.user}
	createVFS(t, f.mod, f.vfs)

	return f
}

func (f *authFixture) authorize(vfs, caller *astral.Identity, want access) error {
	_, err := f.mod.authorizeVFS(testContext(), caller, vfs, want)

	return err
}

func TestAuthorizeVFSIdentityNeedsNoPermit(t *testing.T) {
	f := newAuthFixture(t)

	for _, want := range []access{accessRead, accessWrite} {
		if err := f.authorize(f.vfs, f.vfs, want); err != nil {
			t.Errorf("the VFS identity, access %d: %v", want, err)
		}
	}
	if len(f.auth.asked) != 0 {
		t.Errorf("the auth module was asked %d times for the VFS identity", len(f.auth.asked))
	}
}

func TestAuthorizeRootCallersNeedNoPermit(t *testing.T) {
	f := newAuthFixture(t)

	for name, caller := range map[string]*astral.Identity{"node": f.mod.identity, "user": f.user} {
		for _, want := range []access{accessRead, accessWrite} {
			if err := f.authorize(f.vfs, caller, want); err != nil {
				t.Errorf("%s, access %d: %v", name, want, err)
			}
		}
	}
	if len(f.auth.asked) != 0 {
		t.Errorf("the auth module was asked %d times for root callers", len(f.auth.asked))
	}
}

func TestAuthorizeRootCallerWithoutUserModule(t *testing.T) {
	f := newAuthFixture(t)
	f.mod.User = nil

	if err := f.authorize(f.vfs, f.mod.identity, accessWrite); err != nil {
		t.Errorf("node without a user module: %v", err)
	}
	if err := f.authorize(f.vfs, f.user, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("the user identity without a user module = %v, want not permitted", err)
	}
}

func TestAuthorizeZeroUserIdentityIsNotARootCaller(t *testing.T) {
	f := newAuthFixture(t)
	f.mod.User = fakeUser{id: &astral.Identity{}}

	if err := f.authorize(f.vfs, nil, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("zero caller = %v, want not permitted", err)
	}
	if err := f.authorize(f.vfs, f.mod.identity, accessRead); err != nil {
		t.Errorf("the node stays a root caller: %v", err)
	}
}

func TestAuthorizeZeroCallerIsNeverAllowed(t *testing.T) {
	f := newAuthFixture(t)
	zero := &astral.Identity{}

	// even a permit naming the zero identity does not help it
	f.auth.permit(&vfsapi.ReadAction{}, zero, f.vfs)

	for _, caller := range []*astral.Identity{nil, zero} {
		if err := f.authorize(f.vfs, caller, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
			t.Errorf("caller %v = %v, want not permitted", caller, err)
		}
	}

	// and the zero VFS identity is never a VFS of the caller
	if err := f.authorize(zero, zero, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("zero caller on the zero VFS = %v, want not permitted", err)
	}
}

func TestAuthorizeAsksAuthForThePermitWithTheVFS(t *testing.T) {
	f := newAuthFixture(t)

	f.auth.permit(&vfsapi.ReadAction{}, f.caller, f.vfs)
	if err := f.authorize(f.vfs, f.caller, accessRead); err != nil {
		t.Fatalf("read permit holder: %v", err)
	}

	last := f.auth.asked[len(f.auth.asked)-1].(*vfsapi.ReadAction)
	if !last.Actor().IsEqual(f.caller) || !last.VFSID.IsEqual(f.vfs) {
		t.Fatalf("asked %v for %v, want caller %v on %v", last.Actor(), last.VFSID, f.caller, f.vfs)
	}

	f.auth.permit(&vfsapi.WriteAction{}, f.caller, f.vfs)
	if err := f.authorize(f.vfs, f.caller, accessWrite); err != nil {
		t.Fatalf("write permit holder: %v", err)
	}
	if _, ok := f.auth.asked[len(f.auth.asked)-1].(*vfsapi.WriteAction); !ok {
		t.Fatal("a write was not asked as a write action")
	}
}

func TestAuthorizeWriteDoesNotImplyRead(t *testing.T) {
	f := newAuthFixture(t)

	f.auth.permit(&vfsapi.WriteAction{}, f.caller, f.vfs)

	if err := f.authorize(f.vfs, f.caller, accessWrite); err != nil {
		t.Errorf("write with a write permit: %v", err)
	}
	if err := f.authorize(f.vfs, f.caller, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("read with only a write permit = %v, want not permitted", err)
	}

	g := newAuthFixture(t)
	g.auth.permit(&vfsapi.ReadAction{}, g.caller, g.vfs)
	if err := g.authorize(g.vfs, g.caller, accessWrite); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("write with only a read permit = %v, want not permitted", err)
	}
}

func TestAuthorizePermitIsForOneVFS(t *testing.T) {
	f := newAuthFixture(t)
	other := astral.GenerateIdentity()
	createVFS(t, f.mod, other)

	f.auth.permit(&vfsapi.ReadAction{}, f.caller, f.vfs)

	if err := f.authorize(other, f.caller, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("a permit for one VFS opened another: %v", err)
	}
}

func TestAuthorizeStrangerIsRefused(t *testing.T) {
	f := newAuthFixture(t)

	for _, want := range []access{accessRead, accessWrite} {
		if err := f.authorize(f.vfs, f.caller, want); !errors.Is(err, vfsapi.ErrNotPermitted) {
			t.Errorf("stranger, access %d = %v, want not permitted", want, err)
		}
	}
}

func TestAuthorizeChecksAuthorizationBeforeExistence(t *testing.T) {
	f := newAuthFixture(t)
	missing := astral.GenerateIdentity()

	// a stranger learns nothing about whether a VFS exists
	if err := f.authorize(missing, f.caller, accessRead); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("stranger on a missing VFS = %v, want not permitted", err)
	}

	// an authorized caller learns it
	if err := f.authorize(missing, f.user, accessRead); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Errorf("root caller on a missing VFS = %v, want no such vfs", err)
	}
	if err := f.authorize(missing, missing, accessWrite); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Errorf("the identity of a missing VFS = %v, want no such vfs", err)
	}
}

func TestAuthorizeReturnsTheVolume(t *testing.T) {
	f := newAuthFixture(t)

	vol, err := f.mod.authorizeVFS(testContext(), f.vfs, f.vfs, accessRead)
	if err != nil || vol == nil || !vol.Identity.IsEqual(f.vfs) {
		t.Fatalf("authorizeVFS = %+v, %v", vol, err)
	}
}

func TestAuthorizeManage(t *testing.T) {
	f := newAuthFixture(t)

	for name, caller := range map[string]*astral.Identity{"node": f.mod.identity, "user": f.user} {
		if err := f.mod.authorizeManage(testContext(), caller); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// the VFS identity is no manager
	if err := f.mod.authorizeManage(testContext(), f.vfs); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("VFS identity = %v, want not permitted", err)
	}
	if err := f.mod.authorizeManage(testContext(), nil); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Errorf("zero caller = %v, want not permitted", err)
	}

	f.auth.permit(&vfsapi.ManageAction{}, f.caller, nil)
	if err := f.mod.authorizeManage(testContext(), f.caller); err != nil {
		t.Errorf("manage permit holder: %v", err)
	}
	if _, ok := f.auth.asked[len(f.auth.asked)-1].(*vfsapi.ManageAction); !ok {
		t.Error("the permit was not asked as a manage action")
	}
}
