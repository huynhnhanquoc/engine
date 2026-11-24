package http

// The SSRF guard (http.md mục 4): a request never reaches an address in private or local space, checked as the
// connection opens, after DNS and after every redirect, while developing too.

import (
	"context"
	"errors"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"syscall"
	"time"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/internal/loopback"
)

// deniedAddress is the guard refusing an address.
type deniedAddress struct{ host string }

func (e *deniedAddress) Error() string {
	return "SSRF prevention: a connection to private or local space is blocked (" + e.host + ")"
}

var (
	sharedAddressSpace = mustCIDR("100.64.0.0/10") // carrier-grade NAT, where some clouds keep their metadata
	thisNetwork        = mustCIDR("0.0.0.0/8")     // routed to the host itself by some systems
	nat64              = mustCIDR("64:ff9b::/96")  // the IPv4 address in its last four bytes is the one reached
)

func mustCIDR(text string) *net.IPNet {
	_, network, err := net.ParseCIDR(text)
	if err != nil {
		panic(err)
	}
	return network
}

// Denied says the guard refuses an address: loopback (unless the engine's own tests opened it), private,
// unspecified, link-local, multicast, broadcast, 0.0.0.0/8, 100.64.0.0/10, and the NAT64 address of any of them.
func Denied(ip net.IP) bool {
	if nat64.Contains(ip) && len(ip) == net.IPv6len {
		ip = net.IPv4(ip[12], ip[13], ip[14], ip[15])
	}
	if ip.IsLoopback() {
		return !loopback.Opened()
	}
	return ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) ||
		sharedAddressSpace.Contains(ip) || thisNetwork.Contains(ip)
}

// guardDial runs for every connection the guarded transport opens: address is the IP being dialled.
func guardDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || Denied(ip) {
		return &deniedAddress{host: host}
	}
	return nil
}

// maxRedirects is how many redirects a request follows (http.md mục 4); past them it is NETWORK.
const maxRedirects = 5

var errTooManyRedirects = errors.New("stopped after 5 redirects")

// guardedClient carries every request. It has no time of its own: the request's .timeout() bounds it. It follows five
// redirects at most; on one to another host Go leaves Authorization and Cookie behind.
var guardedClient = &http.Client{
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, Control: guardDial}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	},
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return errTooManyRedirects
		}
		return nil
	},
}

// noAnswer is the failure of a request that got no answer: DENIED when the guard refused the address, TIMEOUT past
// its time, NETWORK for any other (DNS, TLS, a connection refused or broken).
func noAnswer(err error) value.Value {
	var denied *deniedAddress
	var timeout interface{ Timeout() bool }
	switch {
	case errors.As(err, &denied):
		return value.Fail(value.DeniedCode, shownError(err))
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return value.Fail(value.TimeoutCode, "the request timed out (.timeout)")
	}
	return value.Fail(value.NetworkCode, shownError(err))
}

// shownError is a failure as the program reads it: Go names the URL, which can carry a secret, so the URL loses its
// user, its query and its fragment.
func shownError(err error) string {
	var failed *neturl.Error
	if errors.As(err, &failed) {
		shown := *failed
		shown.URL = shownURL(failed.URL)
		return shown.Error()
	}
	return err.Error()
}

// shownURL is raw without its user, its query and its fragment, cut as text (raw may not parse).
func shownURL(raw string) string {
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		raw = raw[:cut]
	}
	if scheme := strings.Index(raw, "://"); scheme >= 0 {
		rest := raw[scheme+3:]
		authority := rest
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			authority = rest[:slash]
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			raw = raw[:scheme+3] + rest[at+1:]
		}
	}
	return raw
}
