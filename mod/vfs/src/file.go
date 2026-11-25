package vfs

import (
	"bytes"
	"io"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// WriteFile creates or replaces the file at path in the VFS of id with content, as vfs.write does for a query.
//
// why: modules inside the node (nodes, for SwarmState) keep files in the node's own VFS without a query.
func (mod *Module) WriteFile(ctx *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error) {
	vol, err := mod.db.findVolume(id)
	if err != nil {
		return nil, err
	}

	names, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, vfsapi.ErrIsAFolder
	}

	allowed, err := mod.computeAllowance(vol, names)
	switch {
	case err != nil:
		return nil, err
	case uint64(len(content)) > allowed:
		return nil, vfsapi.ErrQuotaExceeded
	}

	w, err := mod.openWriter(ctx)
	if err != nil {
		return nil, err
	}
	defer w.Discard()

	if _, err = w.Write(content); err != nil {
		return nil, err
	}

	objectID, err := commitContent(w, uint64(len(content)))
	if err != nil {
		return nil, err
	}

	if _, err = mod.recordFile(vol, names, *objectID); err != nil {
		return nil, err
	}

	return objectID, nil
}

// ReadFile returns the content of the file at path in the VFS of id, from the local group, as vfs.read does.
func (mod *Module) ReadFile(ctx *astral.Context, id *astral.Identity, path string) ([]byte, error) {
	vol, err := mod.db.findVolume(id)
	if err != nil {
		return nil, err
	}

	names, err := parsePath(path)
	if err != nil {
		return nil, err
	}

	objectID, err := mod.findFileID(vol, names)
	if err != nil {
		return nil, err
	}
	if objectID.Size > uint64(objectsmod.MaxObjectSize) {
		return nil, objectsmod.ErrObjectTooLarge
	}

	repo := mod.Objects.GetRepository(readRepository)
	if repo == nil {
		return nil, vfsapi.ErrNoStorage
	}

	r, err := repo.Read(ctx.LimitZone(astral.ZoneDevice), &objectID, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(r, int64(objectID.Size))); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ListFolder returns the names of the entries of the folder at path in the VFS of id, in ascending byte order.
func (mod *Module) ListFolder(id *astral.Identity, path string) ([]string, error) {
	vol, err := mod.db.findVolume(id)
	if err != nil {
		return nil, err
	}

	names, err := parsePath(path)
	if err != nil {
		return nil, err
	}

	rows, err := mod.listFolder(vol, names)
	if err != nil {
		return nil, err
	}

	list := make([]string, 0, len(rows))
	for _, row := range rows {
		list = append(list, row.Name)
	}

	return list, nil
}
