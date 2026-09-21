package objects

import (
	"errors"
	"math"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestResolveReadLimit covers every window rule: the accepted windows, each refusal, the
// empty object, and the bounds where offset+limit would overflow an int64.
func TestResolveReadLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    uint64
		offset  int64
		limit   int64
		want    int64
		wantErr error
	}{
		{"zero limit reads the whole object", 10, 0, 0, 10, nil},
		{"zero limit reads the rest from offset", 10, 4, 0, 6, nil},
		{"limit inside the object", 10, 2, 3, 3, nil},
		{"limit reaching the end exactly", 10, 2, 8, 8, nil},
		{"limit beyond the remaining bytes", 10, 7, 100, 3, nil},
		{"offset equal to size with zero limit", 10, 10, 0, 0, nil},
		{"offset equal to size with a limit", 10, 10, 5, 0, nil},
		{"offset beyond size", 10, 11, 0, 0, ErrOutOfBounds},
		{"offset beyond size with a limit", 10, 11, 1, 0, ErrOutOfBounds},
		{"negative offset", 10, -1, 0, 0, ErrOutOfBounds},
		{"minimum offset", 10, math.MinInt64, 0, 0, ErrOutOfBounds},
		{"negative limit", 10, 0, -1, 0, ErrOutOfBounds},
		{"minimum limit", 10, 0, math.MinInt64, 0, ErrOutOfBounds},
		{"empty object with zero limit", 0, 0, 0, 0, nil},
		{"empty object with a limit", 0, 0, 512, 0, nil},
		{"empty object with an offset", 0, 1, 0, 0, ErrOutOfBounds},
		{"offset 1 and maximum limit", 10, 1, math.MaxInt64, 9, nil},
		{"offset 1 and maximum limit on a maximum size", math.MaxInt64, 1, math.MaxInt64, math.MaxInt64 - 1, nil},
		{"maximum offset and maximum limit on a maximum size", math.MaxInt64, math.MaxInt64, math.MaxInt64, 0, nil},
		{"maximum size", math.MaxInt64, 0, 0, math.MaxInt64, nil},
		{"size one above maximum", math.MaxInt64 + 1, 0, 0, 0, ErrOutOfBounds},
		{"size one above maximum with a window", math.MaxInt64 + 1, 1, 1, 0, ErrOutOfBounds},
		{"largest size", math.MaxUint64, 0, 0, 0, ErrOutOfBounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectID := &astral.ObjectID{Size: tc.size, Hash: [32]byte{1, 2, 3}}
			before := *objectID

			got, err := ResolveReadLimit(objectID, tc.offset, tc.limit)

			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("ResolveReadLimit(%v, %v, %v): unexpected error %v", tc.size, tc.offset, tc.limit, err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("ResolveReadLimit(%v, %v, %v): got error %v, want %v", tc.size, tc.offset, tc.limit, err, tc.wantErr)
			}

			if got != tc.want {
				t.Fatalf("ResolveReadLimit(%v, %v, %v) = %v, want %v", tc.size, tc.offset, tc.limit, got, tc.want)
			}

			if *objectID != before {
				t.Fatalf("ResolveReadLimit changed its argument to %v, want %v", *objectID, before)
			}
		})
	}
}
