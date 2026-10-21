package resources

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
)

var _ Resources = &FileResources{}

type FileResources struct {
	root     string
	dataRoot string
}

func NewFileResources(root string, mkdir bool) (*FileResources, error) {
	fileInfo, err := os.Stat(root)
	if err != nil {
		if !mkdir {
			return nil, err
		}
		err = os.MkdirAll(root, 0750)
		if err != nil {
			return nil, err
		}
	} else if !fileInfo.IsDir() {
		return nil, errors.New("path is not a directory")
	}

	return &FileResources{
		root: root,
	}, nil
}

func (res *FileResources) SetDataRoot(dataRoot string) {
	res.dataRoot = dataRoot
}

func (res *FileResources) Read(name string) ([]byte, error) {
	return readFile(res.root, name)
}

// ReadData reads name from the data root.
func (res *FileResources) ReadData(name string) ([]byte, error) {
	return readFile(res.DataRoot(), name)
}

func readFile(dir, name string) ([]byte, error) {
	bytes, err := os.ReadFile(path.Join(dir, name))

	switch {
	case err == nil:

	case errors.Is(err, fs.ErrNotExist):
		err = ErrNotFound
	}

	return bytes, err
}

func (res *FileResources) Write(name string, data []byte) error {
	if s, _ := os.Stat(res.root); s == nil || !s.IsDir() {
		if err := os.MkdirAll(res.root, 0700); err != nil {
			return fmt.Errorf("cannot create config directory: %w", err)
		}
	}

	return os.WriteFile(path.Join(res.root, name), data, 0600)
}

// WriteData writes data to name in the data root with mode 0600.
//
// why: the data is synced to a temporary file that is then renamed onto name, so
// a crash leaves the previous file or the complete new one, never a partial key.
func (res *FileResources) WriteData(name string, data []byte) error {
	var dir = res.DataRoot()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return err
	}
	// note: after the rename the temporary name is gone and Remove only fails
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path.Join(dir, name))
}

func (res *FileResources) Root() string {
	return res.root
}

func (res *FileResources) DataRoot() string {
	if len(res.dataRoot) > 0 {
		return res.dataRoot
	}

	return res.Root()
}
