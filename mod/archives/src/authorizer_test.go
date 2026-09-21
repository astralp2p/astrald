package archives

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// TestAuthorizeSeeObjectsGrantsOnAnyParentArchive: an entry indexed in two
// archives is readable when the actor can read either of them. The lookup
// carries no ORDER BY, so deciding on the first row makes the verdict depend on
// an unspecified row order -- one of these two subtests fails whichever order
// sqlite happens to return.
func TestAuthorizeSeeObjectsGrantsOnAnyParentArchive(t *testing.T) {
	actor := astral.GenerateIdentity()
	entryID := testObjectID(t, "entry")
	archiveA := testObjectID(t, "archive-a")
	archiveB := testObjectID(t, "archive-b")

	for _, tc := range []struct {
		name    string
		granted *astral.ObjectID
	}{
		{"only the first archive grants", archiveA},
		{"only the second archive grants", archiveB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := newTestModule(t)
			indexEntry(t, mod, archiveA, entryID, "a.txt")
			indexEntry(t, mod, archiveB, entryID, "b.txt")
			mod.Auth = grantingAuth{granted: map[string]bool{tc.granted.String(): true}}

			action := &auth.SeeObjectsAction{
				Action:   auth.NewAction(actor),
				ObjectID: entryID,
			}
			if !mod.AuthorizeSeeObjects(nil, action) {
				t.Error("AuthorizeSeeObjects denied an entry whose parent archive the actor may read")
			}
		})
	}
}

// TestAuthorizeSeeObjectsRefusesWhenNoParentGrants pins that the fix did not
// degenerate into an unconditional grant.
func TestAuthorizeSeeObjectsRefusesWhenNoParentGrants(t *testing.T) {
	mod := newTestModule(t)
	entryID := testObjectID(t, "entry")
	indexEntry(t, mod, testObjectID(t, "archive-a"), entryID, "a.txt")
	indexEntry(t, mod, testObjectID(t, "archive-b"), entryID, "b.txt")
	mod.Auth = grantingAuth{granted: map[string]bool{}}

	action := &auth.SeeObjectsAction{
		Action:   auth.NewAction(astral.GenerateIdentity()),
		ObjectID: entryID,
	}
	if mod.AuthorizeSeeObjects(nil, action) {
		t.Error("AuthorizeSeeObjects granted an entry with no readable parent archive")
	}
}

// TestAuthorizeSeeObjectsRefusesAnActionWithoutAnObjectID: SeeObjects also
// covers ops naming no object, and an archive says nothing about those.
func TestAuthorizeSeeObjectsRefusesAnActionWithoutAnObjectID(t *testing.T) {
	mod := newTestModule(t)
	mod.Auth = grantingAuth{granted: map[string]bool{}}

	action := &auth.SeeObjectsAction{Action: auth.NewAction(astral.GenerateIdentity())}
	if mod.AuthorizeSeeObjects(nil, action) {
		t.Error("AuthorizeSeeObjects granted an action naming no object")
	}
}
