package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestFromFlag(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	cwd := t.TempDir()
	t.Chdir(cwd)

	cases := []struct{ in, want string }{
		{"/a/b/../c", "/a/c"},
		{"~/node", "/home/u/node"},
		{"relnode", filepath.Join(cwd, "relnode")},
		{"./x/", filepath.Join(cwd, "x")},
	}
	for _, c := range cases {
		got, err := FromFlag(c.in)
		if err != nil || got != c.want {
			t.Errorf("FromFlag(%q) = (%q, %v); want (%q, nil)", c.in, got, err, c.want)
		}
	}
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"/x/share/", "/x/share", true},
		{"", "", false},
		{"rel", "", false},
		{"~/x", "", false},
	}
	for _, c := range cases {
		got, ok := FromEnv(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("FromEnv(%q) = (%q, %v); want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFromConfig(t *testing.T) {
	t.Setenv("HOME", "/home/u")

	for in, want := range map[string]string{
		"/etc/x/../token": "/etc/token",
		"~/token":         "/home/u/token",
	} {
		got, err := FromConfig(in)
		if err != nil || got != want {
			t.Errorf("FromConfig(%q) = (%q, %v); want (%q, nil)", in, got, err, want)
		}
	}

	for _, in := range []string{"token", "./token", ""} {
		if _, err := FromConfig(in); !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("FromConfig(%q) error = %v; want ErrNotAbsolute", in, err)
		}
	}
}

func TestFromConfigRelativeHome(t *testing.T) {
	t.Setenv("HOME", "rel")

	if _, err := FromConfig("~/token"); !errors.Is(err, ErrNotAbsolute) {
		t.Fatalf("FromConfig(~/token) with HOME=rel error = %v; want ErrNotAbsolute", err)
	}
}

func TestFromConfigAddr(t *testing.T) {
	t.Setenv("HOME", "/home/u")

	for in, want := range map[string]string{
		"unix:~/.apphost.sock": "unix:/home/u/.apphost.sock",
		"unix:/run/a.sock":     "unix:/run/a.sock",
		"tcp:127.0.0.1:8625":   "tcp:127.0.0.1:8625",
		"memu:apphosty":        "memu:apphosty",
	} {
		got, err := FromConfigAddr(in)
		if err != nil || got != want {
			t.Errorf("FromConfigAddr(%q) = (%q, %v); want (%q, nil)", in, got, err, want)
		}
	}

	if _, err := FromConfigAddr("unix:a.sock"); !errors.Is(err, ErrNotAbsolute) {
		t.Fatalf("FromConfigAddr(unix:a.sock) error = %v; want ErrNotAbsolute", err)
	}
}

func TestCheckAddr(t *testing.T) {
	for _, in := range []string{"unix:/tmp/a.sock", "tcp:127.0.0.1:1"} {
		if err := CheckAddr(in); err != nil {
			t.Errorf("CheckAddr(%q) = %v; want nil", in, err)
		}
	}
	for _, in := range []string{"unix:a.sock", "unix:~/a.sock"} {
		if err := CheckAddr(in); !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("CheckAddr(%q) = %v; want ErrNotAbsolute", in, err)
		}
	}
}
