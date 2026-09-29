package reactions

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"

	"github.com/sky-valley/pagelike/internal/site"
)

// Destination policy for outbound requests (R-REACT-61, a pagelike
// security decision): private destinations — loopback, link-local
// (169.254.169.254 included), RFC 1918, CGNAT, IPv6 ULA, unspecified and
// multicast addresses — are refused unless the instance or the site allows
// them, either wholesale (allow_private) or by name, address, address:port
// or CIDR prefix. Every address a name resolves to is checked when the
// connection is made (a DNS-rebinding guard). Requests to sites this
// server serves are delivered in process and are always allowed. A
// refused destination is a failed attempt.

var instancePolicy struct {
	sync.RWMutex
	allowPrivate bool
	allow        []string
}

// SetOutboundPolicy sets the instance-wide destination policy (the serve
// command's --outbound-allow-private and --outbound-allow flags).
func SetOutboundPolicy(allowPrivate bool, allow []string) {
	instancePolicy.Lock()
	instancePolicy.allowPrivate = allowPrivate
	instancePolicy.allow = append([]string(nil), allow...)
	instancePolicy.Unlock()
}

type policy struct {
	allowPrivate bool
	allow        []allowEntry
}

type allowEntry struct {
	prefix netip.Prefix // valid for address and CIDR entries
	host   string       // a host name entry (lower case)
	port   string       // "" = any port
}

func policyFor(s *site.Site) *policy {
	p := &policy{}
	instancePolicy.RLock()
	p.allowPrivate = instancePolicy.allowPrivate
	entries := append([]string(nil), instancePolicy.allow...)
	instancePolicy.RUnlock()
	if s != nil {
		if o := s.Settings().Outbound; o != nil {
			p.allowPrivate = p.allowPrivate || o.AllowPrivate
			entries = append(entries, o.Allow...)
		}
	}
	for _, e := range entries {
		if ae, ok := parseAllow(e); ok {
			p.allow = append(p.allow, ae)
		}
	}
	return p
}

func parseAllow(s string) (allowEntry, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return allowEntry{}, false
	}
	if pr, err := netip.ParsePrefix(s); err == nil {
		return allowEntry{prefix: pr.Masked()}, true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return allowEntry{prefix: netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())}, true
	}
	host, port := s, ""
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return allowEntry{prefix: netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), port: port}, true
	}
	return allowEntry{host: strings.ToLower(strings.TrimSuffix(host, ".")), port: port}, true
}

// hostAllowed reports an allowlist entry naming the URL's host.
func (p *policy) hostAllowed(u *url.URL) bool {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	for _, e := range p.allow {
		if e.host != "" && e.host == host && (e.port == "" || e.port == port) {
			return true
		}
	}
	return false
}

func (p *policy) addrAllowed(a netip.Addr, port string) bool {
	for _, e := range p.allow {
		if e.prefix.IsValid() && e.prefix.Contains(a) && (e.port == "" || e.port == port) {
			return true
		}
	}
	return false
}

// checkHost refuses a URL before any connection when its host is a
// literal private address that is not allowed.
func (p *policy) checkHost(u *url.URL) error {
	if p.allowPrivate || p.hostAllowed(u) {
		return nil
	}
	a, err := netip.ParseAddr(u.Hostname())
	if err != nil {
		return nil // a name: checked per resolved address
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	return p.checkAddr(a.Unmap(), port)
}

func (p *policy) checkAddr(a netip.Addr, port string) error {
	if !privateAddr(a) || p.addrAllowed(a, port) {
		return nil
	}
	return fmt.Errorf("outbound destination %s is a private address and is not allowed (R-REACT-61)", net.JoinHostPort(a.String(), port))
}

// control checks every address the dialer connects to.
func (p *policy) control(u *url.URL) func(network, address string, c syscall.RawConn) error {
	byName := p.allowPrivate || p.hostAllowed(u)
	return func(network, address string, _ syscall.RawConn) error {
		if byName {
			return nil
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		a, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("outbound destination %s: %v", address, err)
		}
		return p.checkAddr(a.Unmap(), port)
	}
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")
var thisNet = netip.MustParsePrefix("0.0.0.0/8")

func privateAddr(a netip.Addr) bool {
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() || cgnat.Contains(a) || thisNet.Contains(a)
}
