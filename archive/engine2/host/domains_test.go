package host

import (
	"context"
	"testing"
)

// AutoSSL asks for a certificate of a site the boot opened and of a hostname the table of domains sends elsewhere, which answers
// over HTTPS too (its 301); never of a row it neither serves nor sends anywhere, nor of a name it does not hold.
//
// The www. or bare name of a site, which the www rule sends to it (15/11/2026), has its certificate too: www.shop.example
// and blog.example; not www.else.example, whose pair is no site, nor www.old.example, a row of the table's.
//
// CONTROL: with HostPolicy reading the sites alone, old.example had no certificate and its 301 no HTTPS to come by.
func TestAutoSSLAsksForTheTablesHostnames(t *testing.T) {
	h := &Host{Folder: t.TempDir()}
	h.live.Store(&map[string]*Site{"shop.example": nil, "www.blog.example": nil})
	h.domains.Store(&domainTable{rows: map[string]domainRow{
		"shop.example":     {identity: "bbb", status: "confirmed"},
		"www.blog.example": {identity: "bbb", status: "confirmed"},
		"old.example":      {identity: "bbb", status: "redirect", redirect: "shop.example"},
		"hold.example":     {identity: "bbb", status: "unverified", redirect: "shop.example"},
		"parked.example":   {identity: "bbb", status: "redirect"},
		"www.old.example":  {identity: "bbb", status: "unverified"},
	}})
	policy := newCertificates(h, "", "").manager.HostPolicy
	for hostname, want := range map[string]bool{"shop.example": true, "OLD.example": true, "hold.example": false, "parked.example": false, "else.example": false,
		"www.shop.example": true, "blog.example": true, "www.else.example": false, "www.old.example": false} {
		if got := policy(context.Background(), hostname) == nil; got != want {
			t.Errorf("%s: a certificate %v, want %v", hostname, got, want)
		}
	}
}

// redirect_to sends a request to an http:// or https:// address, or a hostname with a port and a path or not;
// anything else is no address, and its row sends nowhere.
func TestWhereARedirectMaySend(t *testing.T) {
	for to, ok := range map[string]bool{
		"shop.example": true, "shop.example/landing": true, "shop.example:8443/a": true, "https://shop.example/a?b": true,
		"http://shop.example": true, "javascript:alert(1)": false, "ftp://shop.example": false, "https://": false,
		"shop example": false, "shop.example:0": false, "shop.example:x": false, "shöp.example": false, "": false,
	} {
		if got := redirectProblem(to) == ""; got != ok {
			t.Errorf("%q: an address %v, want %v (%s)", to, got, ok, redirectProblem(to))
		}
	}
}

// A local name is no one's: the machine's, a test's, a loopback or private address. A public name or address is
// someone's, and the table says whose.
func TestALocalNameIsNoOnes(t *testing.T) {
	for name, local := range map[string]bool{
		"localhost": true, "kitwork.io.localhost": true, "shop.test": true, "127.0.0.1": true, "10.1.2.3": true,
		"192.168.1.9": true, "169.254.1.1": true, "kitwork.io": false, "shop.example": false, "8.8.8.8": false,
		"test.example": false, "localhost.example": false,
	} {
		if localName(name) != local {
			t.Errorf("%s: local %v, want %v", name, !local, local)
		}
	}
}
