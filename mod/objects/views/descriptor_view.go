package objects

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type DescriptorView struct {
	*objects.Descriptor
}

func (view DescriptorView) Render() string {
	return fmt.Sprintf("%v %v", "➤", view.Data)
}

func init() {
	fmt.SetView(func(o *objects.Descriptor) fmt.View {
		return &DescriptorView{Descriptor: o}
	})
}
