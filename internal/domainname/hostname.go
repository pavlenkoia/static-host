// Package domainname provides canonical DNS hostname handling.
package domainname

import (
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/net/idna"
)

var profile = idna.New(idna.MapForLookup(), idna.StrictDomainName(true), idna.ValidateLabels(true), idna.VerifyDNSLength(true))

// Normalize returns a lowercase ASCII DNS hostname, without a trailing dot.
func Normalize(hostname string) (string, error) {
	hostname = strings.TrimSuffix(strings.TrimSpace(hostname), ".")
	if hostname == "" || strings.ContainsAny(hostname, "/:*@?#[]\\") {
		return "", errors.New("invalid domain")
	}
	ascii, err := profile.ToASCII(hostname)
	if err != nil {
		return "", errors.New("invalid domain")
	}
	ascii = strings.ToLower(ascii)
	if _, err := netip.ParseAddr(ascii); err == nil || !strings.Contains(ascii, ".") {
		return "", errors.New("invalid domain")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid domain")
		}
	}
	return ascii, nil
}
