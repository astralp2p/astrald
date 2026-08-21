package dir

import (
	"fmt"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
	"net"
	"regexp"
	"strings"
)

var domainRegex = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9][a-z0-9-]{0,61}[a-z0-9]$`)
var _ dir.Resolver = &DNS{}

// DNS resolves identities from DNS TXT records published under the _astral.<domain> subdomain.
type DNS struct {
	*Module
}

func isValidDomain(domain string) bool {
	return domainRegex.MatchString(domain)
}

// ResolveIdentity looks up the identity published in a TXT record at _astral.<s> with the prefix "id=".
func (dns DNS) ResolveIdentity(s string) (identity *astral.Identity, err error) {
	if !isValidDomain(s) {
		return &astral.Identity{}, fmt.Errorf("cannot resolve")
	}

	domain := "_astral." + s
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		dns.log.Errorv(1, "error looking up TXT records for %v: %v", domain, err)
		return identity, err
	}

	for _, record := range txtRecords {
		if !strings.HasPrefix(record, "id=") {
			continue
		}

		identity, err = astral.ParseIdentity(record[3:])
		if err == nil {
			return identity, nil
		}
	}

	return &astral.Identity{}, fmt.Errorf("cannot resolve")
}

// DisplayName always returns an empty string; DNS resolution provides no human-readable display names.
func (dns DNS) DisplayName(identity *astral.Identity) string {
	return ""
}
