package mobile

import (
	"errors"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// OpenObject opens the object's data through the node's objects module,
// starting at offset. The platform wrapper uses this to stream media to
// the system player. The returned reader is positioned at offset and
// reads until the end of the object; seeking is done by reopening at a
// new offset.
//
// The read uses the node's identity and all zones, so objects available
// over the network (but not stored locally) work too.
func (n *Node) OpenObject(objectID string, offset int64) (*ObjectReader, error) {
	n.mu.Lock()
	cnode := n.cnode
	n.mu.Unlock()

	if cnode == nil || !n.running.Load() {
		return nil, errors.New("node not running")
	}

	id, err := astral.ParseID(objectID)
	if err != nil {
		return nil, err
	}

	mod, err := core.Load[objects.Module](cnode, objects.ModuleName)
	if err != nil {
		return nil, err
	}

	ctx := astral.NewContext(nil).
		WithIdentity(cnode.Identity()).
		WithZone(astral.ZoneAll)

	r, err := mod.ReadDefault().Read(ctx, id, offset, 0)
	if err != nil {
		return nil, err
	}

	return &ObjectReader{r: r, size: int64(id.Size)}, nil
}

// CreateObject opens a writer into the node's default write repository.
// The platform wrapper uses this to store objects it produces (e.g. cover
// art extracted from media tags). Write the data, then Commit to get the
// object ID, or Discard to drop it.
func (n *Node) CreateObject() (*ObjectWriter, error) {
	n.mu.Lock()
	cnode := n.cnode
	n.mu.Unlock()

	if cnode == nil || !n.running.Load() {
		return nil, errors.New("node not running")
	}

	mod, err := core.Load[objects.Module](cnode, objects.ModuleName)
	if err != nil {
		return nil, err
	}

	ctx := astral.NewContext(nil).WithIdentity(cnode.Identity())

	w, err := mod.WriteDefault().Create(ctx, &objects.CreateOpts{})
	if err != nil {
		return nil, err
	}

	return &ObjectWriter{w: w}, nil
}

// ObjectWriter writes a new object into the node's repository. Not safe
// for concurrent use.
type ObjectWriter struct {
	w objects.Writer
}

// Write appends len(buf) bytes to the object.
func (w *ObjectWriter) Write(buf []byte) (int, error) {
	return w.w.Write(buf)
}

// Commit stores the object and returns its ID. Closes the writer.
func (w *ObjectWriter) Commit() (string, error) {
	id, err := w.w.Commit()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// Discard drops the data written so far and closes the writer.
func (w *ObjectWriter) Discard() {
	_ = w.w.Discard()
}

// ObjectReader streams an object's data. Not safe for concurrent use.
type ObjectReader struct {
	r    io.ReadCloser
	size int64
}

// Read fills buf with up to len(buf) bytes and returns the number of
// bytes read. A return of 0 means end of data (gomobile maps Go errors
// to exceptions, so EOF is signaled in-band instead).
func (r *ObjectReader) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	for {
		n, err := r.r.Read(buf)
		if n > 0 {
			return n, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, nil
			}
			return 0, err
		}
	}
}

// Size returns the total size of the object in bytes.
func (r *ObjectReader) Size() int64 {
	return r.size
}

// Close releases the underlying reader.
func (r *ObjectReader) Close() error {
	return r.r.Close()
}
