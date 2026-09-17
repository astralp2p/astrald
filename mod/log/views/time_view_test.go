package views

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

func TestTimeViewRender(t *testing.T) {
	// note: astral.Time.Time() is UTC, so the instant is built in UTC to read the same everywhere.
	at := astral.Time(time.Date(2024, 3, 5, 12, 34, 56, 789e6, time.UTC))

	for _, c := range []struct {
		name string
		view fmt.View
		want string
	}{
		{"default layout", NewTimeView(&at), "12:34:56.789"},
		{"long layout", NewTimeViewColor(&at, LongTimeLayout, theme.Time), "2024-03-05 12:34:56"},
		{"empty layout", TimeView{Time: &at}, "12:34:56.789"},
	} {
		if got := ansi.ReplaceAllString(c.view.Render(), ""); got != c.want {
			t.Errorf("%s: Render() = %q, want %q", c.name, got, c.want)
		}
	}
}
