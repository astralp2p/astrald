package nearby

import (
	"strconv"

	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/styles"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astrald/mod/nearby"
)

type StatusView struct {
	*nearby.Status
}

func (v StatusView) Render() string {
	s := fmt.Sprint(v.Identity)
	l := len(v.Attachments.Objects())
	if l > 0 {
		var first = true
		s += " ("
		for _, a := range v.attachments() {
			if !first {
				s += ", "
			}
			s += a
			first = false
		}
		s += ")"
	}

	return s
}

func init() {
	fmt.SetView(func(o *nearby.Status) fmt.View {
		return StatusView{Status: o}
	})
}

func (v StatusView) attachments() (list []string) {
	var more int
	var alias string
	var flags []string
	var endpoints int

	for _, attachment := range v.Attachments.Objects() {
		switch a := attachment.(type) {
		case *dir.Alias:
			alias = a.String()
		case *nearby.Flag:
			flags = append(flags, a.String())
		case *nodes.EndpointWithTTL:
			endpoints++
		default:
			more++
		}
	}

	if len(alias) > 0 {
		list = append(list, theme.Identity.Render(alias))
	}

	if len(flags) > 0 {
		for _, flag := range flags {
			list = append(list, theme.Quaternary.Render(flag))
		}
	}

	if endpoints > 0 {
		item := theme.Normal.Render(strconv.Itoa(endpoints))
		item += styles.Gray.Render(" endpoints")
		list = append(list, item)
	}

	if more > 0 {
		item := styles.White.Render(strconv.Itoa(more))
		item += styles.Gray.Render(" other attachment")
		list = append(list, item)
	}

	return
}
