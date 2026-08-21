package objects

import (
	stdfmt "fmt"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/styles"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astrald/mod/log/views"
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
