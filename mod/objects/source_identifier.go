package objects

import "github.com/astralp2p/astral-go/astral"

// SourceIdentifier marks discovery adapters by their source identity;
// "external" may be the better conceptual name.
type SourceIdentifier interface {
	SourceIdentity() *astral.Identity
}

// SourceIdentity extracts the source identity from v if it implements SourceIdentifier.
// The bool reports whether v implements the interface; err is non-nil only when it does
// but yields a nil or invalid identity.
func SourceIdentity(v any) (*astral.Identity, bool, error) {
	source, ok := v.(SourceIdentifier)
	if !ok {
		return nil, false, nil
	}

	if source == nil {
		return nil, true, ErrNilSourceIdentifier
	}

	id := source.SourceIdentity()
	if id == nil || id.IsZero() {
		return nil, true, ErrInvalidSourceIdentity
	}

	return id, true, nil
}
