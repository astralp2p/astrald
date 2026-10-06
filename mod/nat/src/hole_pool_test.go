package nat

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	natmod "github.com/astralp2p/astrald/mod/nat"
)

func poolHole(nonce astral.Nonce, active, passive *astral.Identity) *Hole {
	return NewHoleWithConn(nat.Hole{
		Nonce:           nonce,
		ActiveIdentity:  active,
		PassiveIdentity: passive,
	}, active, false, nil)
}

func TestHolePoolAddRejectsDuplicateNonce(t *testing.T) {
	pool := NewHolePool(nil)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := pool.Add(poolHole(1, a, b)); err != nil {
		t.Fatalf("Add(nonce 1) = %v; want nil", err)
	}
	if err := pool.Add(poolHole(1, b, a)); !errors.Is(err, natmod.ErrDuplicateHole) {
		t.Errorf("Add(second nonce 1) = %v; want %v", err, natmod.ErrDuplicateHole)
	}
}

func TestHolePoolGetAndTake(t *testing.T) {
	pool := NewHolePool(nil)
	h1 := poolHole(1, astral.GenerateIdentity(), astral.GenerateIdentity())
	if err := pool.Add(h1); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got, ok := pool.Get(1); !ok || got != h1 {
		t.Fatalf("Get(1) = (%p, %v); want (%p, true)", got, ok, h1)
	}

	got, err := pool.Take(1)
	if err != nil || got != h1 {
		t.Fatalf("Take(1) = (%p, %v); want (%p, nil)", got, err, h1)
	}
	if _, ok := pool.Get(1); ok {
		t.Error("Get(1) after Take = true; want false")
	}
	if _, err := pool.Take(1); !errors.Is(err, natmod.ErrHoleNotExists) {
		t.Errorf("second Take(1) = %v; want %v", err, natmod.ErrHoleNotExists)
	}
}

func TestHolePoolTakeAnyMatchesEitherSide(t *testing.T) {
	pool := NewHolePool(nil)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	c, d := astral.GenerateIdentity(), astral.GenerateIdentity()

	for _, h := range []*Hole{poolHole(1, a, b), poolHole(2, c, d)} {
		if err := pool.Add(h); err != nil {
			t.Fatalf("Add(nonce %v): %v", h.Nonce, err)
		}
	}

	h, err := pool.TakeAny(b)
	if err != nil {
		t.Fatalf("TakeAny(B) = %v; want the nonce 1 hole", err)
	}
	if h.Nonce != 1 {
		t.Fatalf("TakeAny(B) took nonce %v; want 1", h.Nonce)
	}

	all := pool.GetAll()
	if len(all) != 1 || all[0].Nonce != 2 {
		t.Fatalf("GetAll() after TakeAny(B) has %d holes; want only nonce 2", len(all))
	}

	if _, err := pool.TakeAny(a); !errors.Is(err, natmod.ErrHoleNotExists) {
		t.Errorf("TakeAny(A) after its hole was taken = %v; want %v", err, natmod.ErrHoleNotExists)
	}

	h, err = pool.TakeAny(d)
	if err != nil {
		t.Fatalf("TakeAny(D) = %v; want the nonce 2 hole", err)
	}
	if h.Nonce != 2 {
		t.Errorf("TakeAny(D) took nonce %v; want 2", h.Nonce)
	}
}

func TestHolePoolRemoveMissing(t *testing.T) {
	pool := NewHolePool(nil)

	if h, ok := pool.Remove(99); h != nil || ok {
		t.Errorf("Remove(99) = (%p, %v); want (nil, false)", h, ok)
	}
}
