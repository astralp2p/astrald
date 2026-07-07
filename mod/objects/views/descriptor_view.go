package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
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
