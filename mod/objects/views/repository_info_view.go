package objects

import (
	stdfmt "fmt"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/log/views"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type RepositoryInfoView struct {
	*objects.RepositoryInfo
}

func (v RepositoryInfoView) Render() string {
	var size = astral.Size(v.Free)

	return stdfmt.Sprintf("%s: %s (%s free)",
		theme.Primary.Render(string(v.Name)),
		styles.White.Render(string(v.Label)),
		views.SizeView{Size: &size}.Render(),
	)
}

func init() {
	fmt.SetView(func(o *objects.RepositoryInfo) fmt.View {
		return &RepositoryInfoView{RepositoryInfo: o}
	})
}
