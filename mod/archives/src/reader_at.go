package archives

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

const openTimeout = 15 * time.Second

type readerAt struct {
	identity *astral.Identity
	objects  objects.Module
	objectID *astral.ObjectID
}

func (r *readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	ctx, cancel := astral.NewContext(nil).WithIdentity(r.identity).WithTimeout(openTimeout)
	defer cancel()

	f, err := r.objects.ReadDefault().Read(ctx, r.objectID, off, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	return f.Read(p)
}
