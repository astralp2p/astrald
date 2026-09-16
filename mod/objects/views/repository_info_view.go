package objects

import (
	stdfmt "fmt"
	"strings"

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

	line := stdfmt.Sprintf("%s: %s (%s free)",
		theme.Primary.Render(string(v.Name)),
		styles.White.Render(string(v.Label)),
		views.SizeView{Size: &size}.Render(),
	)

	if v.Kind != objects.RepositoryKindGroup {
		return line
	}

	children := make([]string, 0, len(v.Children))
	for _, child := range v.Children {
		children = append(children, theme.Primary.Render(string(child)))
	}

	return stdfmt.Sprintf("%s group [%s] concurrent: %s",
		line,
		strings.Join(children, ", "),
		views.BoolView{Bool: &v.Concurrent}.Render(),
	)
}

func init() {
	fmt.SetView(func(o *objects.RepositoryInfo) fmt.View {
		return &RepositoryInfoView{RepositoryInfo: o}
	})
}
