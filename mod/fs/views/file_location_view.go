package fs

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/fs"
)

type FileLocationView struct {
	*fs.FileLocation
}

func (v *FileLocationView) Render() string {
	return fmt.Sprintf(
		"file at %v:%v",
		v.NodeID,
		styles.String(string(v.Path), theme.Tertiary.Bri(theme.More)),
	)
}

func init() {
	fmt.SetView(func(o *fs.FileLocation) fmt.View {
		return &FileLocationView{FileLocation: o}
	})
}
