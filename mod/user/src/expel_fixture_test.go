package user

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testDB(t testing.TB) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbMnemonic{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func sampleSigned(issuer, subject *astral.Identity) *user.SignedExpulsion {
	return &user.SignedExpulsion{
		Expulsion: &user.Expulsion{
			Issuer:  issuer,
			Subject: subject,
			// sub-second precision so the test catches a column that truncates it
			ExpelledAt: astral.Time(time.Unix(1700000000, 123456789).UTC()),
		},
		IssuerSig: &crypto.Signature{
			Scheme: crypto.SchemeASN1,
			Data:   astral.Bytes16("dummy-signature-bytes"),
		},
	}
}

// StoreExpulsion is the test fixture the intern0-owned authorize*_test.go files call to ban a node. The ban
// table is gone; the Expulsion issuer signed on the subject goes where the module's swarm view reads it.
// Set mod on the DB (banModule does) before calling it.
func (db *DB) StoreExpulsion(s *user.SignedExpulsion) error {
	return storeBan(db.mod, sampleSignedValid(s.Expulsion.Issuer, s.Expulsion.Subject))
}

// routelessNode is an identityNode that routes no query: a swarm pull over it fails, as one to an unreachable
// peer does.
type routelessNode struct{ identityNode }

func (n *routelessNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return nil, errors.New("no route")
}
