package arl

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
	"regexp"
	"strings"
)

var callerExp = regexp.MustCompile(`^([a-zA-Z0-9_.-]+)@`)
var targetExp = regexp.MustCompile(`^([a-zA-Z0-9_.-]+):`)
var queryExp = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:(.*)$`)

// ARL - Astral Resource Locator
type ARL struct {
	Caller *astral.Identity
	Target *astral.Identity
	Query  string
}

func New(caller *astral.Identity, target *astral.Identity, query string) *ARL {
	return &ARL{Caller: caller, Target: target, Query: query}
}

// Split parses a raw ARL string of the form [caller@][target:]query into its three components.
func Split(s string) (caller, target, query string) {
	matches := callerExp.FindStringSubmatch(s)
	if len(matches) > 0 {
		s, _ = strings.CutPrefix(s, matches[0])
		caller = matches[1]
	}

	matches = targetExp.FindStringSubmatch(s)
	if len(matches) > 0 {
		s, _ = strings.CutPrefix(s, matches[0])
		target = matches[1]
	}

	query = s

	return
}

// Parse parses an ARL string (with or without the astral:// scheme) into an ARL; if resolver is non-nil it is used to resolve identity strings, otherwise raw key parsing is attempted.
func Parse(s string, resolver dir.Resolver) (arl *ARL, err error) {
	if after, found := strings.CutPrefix(s, "astral://"); found {
		s = after
	}

	var c, t string
	arl = &ARL{}
	c, t, arl.Query = Split(s)

	if len(c) != 0 {
		if resolver != nil {
			arl.Caller, err = resolver.ResolveIdentity(c)
			if err != nil {
				return
			}
		} else {
			arl.Caller, err = astral.ParseIdentity(c)
			if err != nil {
				return
			}
		}
	}

	if len(t) != 0 {
		if resolver != nil {
			arl.Target, err = resolver.ResolveIdentity(t)
			if err != nil {
				return
			}
		} else {
			arl.Target, err = astral.ParseIdentity(t)
			if err != nil {
				return
			}
		}
	}

	return
}

func (arl *ARL) String() (s string) {
	if !arl.Caller.IsZero() {
		s = arl.Caller.String() + "@"
	}
	if !arl.Target.IsZero() {
		s = s + arl.Target.String() + ":"
	}
	if len(arl.Query) > 0 {
		s = s + arl.Query
	}
	return
}
