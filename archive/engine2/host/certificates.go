package host

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// certificatesFolder is where the host keeps its sites' certificates and its Let's Encrypt account key:
// beside the manifest, out of git (.gitignore), made 0700 by autocert when it writes its first file.
const certificatesFolder = ".certificates"

// Certificates is AutoSSL, as the first engine had it (engine/domain/ssl.go): a certificate for each site,
// asked of Let's Encrypt the first time the site is reached over HTTPS and renewed before it expires, by
// golang.org/x/crypto/acme/autocert. Which names it asks for is the closed set of site folders Boot opened
// (docs/KITWORK_BOOT.md mục 4): a Host no folder names never reaches Let's Encrypt, so no request can spend
// the host's quota of certificates on a name it does not serve. No list to keep and no database: the
// folder is the hostname (docs/KITWORK_UI.md mục 4).
type Certificates struct {
	manager *autocert.Manager
}

// newCertificates is AutoSSL for the sites of h, its certificates kept in .certificates/ beside the
// manifest, Let's Encrypt writing to email ("" for none). authority is the ACME directory, Let's
// Encrypt's when "".
func newCertificates(h *Host, email, authority string) *Certificates {
	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(filepath.Join(h.Folder, certificatesFolder)),
		Email:  email,
		// autocert asks this before it reads its cache, so a name that is no site costs no read either
		HostPolicy: func(_ context.Context, hostname string) error {
			name := strings.ToLower(hostname)
			if _, ok := h.current()[name]; ok {
				return nil
			}
			if h.domains.Load().redirect(name) != "" {
				return nil // a hostname the table of domains sends elsewhere: it answers over HTTPS too (domains.go)
			}
			if h.paired(name) != "" {
				return nil // the www. or bare name of a site, which the www rule sends to it (domains.go)
			}
			return errors.New("no site of this host is named " + hostname)
		},
	}
	if authority != "" {
		manager.Client = &acme.Client{DirectoryURL: authority}
	}
	return &Certificates{manager: manager}
}

// TLSConfig is what the HTTPS server answers a handshake with: the site's certificate, HTTP/2, and the
// tls-alpn-01 challenge Let's Encrypt may use on 443. TLS 1.2 at least.
func (c *Certificates) TLSConfig() *tls.Config {
	config := c.manager.TLSConfig()
	config.MinVersion = tls.VersionTLS12
	return config
}

// ChallengeHandler is what port 80 answers: Let's Encrypt's http-01 challenge for a site, and for any
// other request a redirect of GET and HEAD to the same address over https (a 400 for the rest), so a
// page is never served without TLS where TLS is on.
func (c *Certificates) ChallengeHandler() http.Handler {
	return c.manager.HTTPHandler(nil)
}

// firstEnginesFolder is where the first engine kept the same files, autocert's too (engine/domain/ssl.go), beside the
// manifest it ran from.
const firstEnginesFolder = "certs"

// adoptFirstEngines copies the certificates and the Let's Encrypt account key of the first engine, certs/ beside the
// manifest, into .certificates/ the first time engine2 serves HTTPS there: the files are autocert's in both, so a host
// moving from the first engine keeps its certificates and its account, where it asked Let's Encrypt again, with a new
// account, for every name it serves as each was first reached (15/11/2026, round 2 of the 13/11 review: the table of
// domains holds 286 hostnames, and Let's Encrypt bounds the orders of an account). Only once: a .certificates/
// that is there, empty or not, is engine2's own, and certs/ is never read again; certs/ is left as it was. A file
// that is no regular file (a folder, a link) is not copied; one that cannot be is said, and autocert asks for it
// again.
func (h *Host) adoptFirstEngines() {
	to := filepath.Join(h.Folder, certificatesFolder)
	if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return
	}
	from := filepath.Join(h.Folder, firstEnginesFolder)
	entries, err := os.ReadDir(from)
	if err != nil || len(entries) == 0 {
		return
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		h.log.Error("[app] ", "AutoSSL: "+certificatesFolder+"/ cannot be made for the first engine's certificates: "+err.Error())
		return
	}
	copied := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if err := copyFile(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
			h.log.Error("[app] ", "AutoSSL: "+firstEnginesFolder+"/"+entry.Name()+" is not copied: "+err.Error())
			continue
		}
		copied++
	}
	h.log.Line("[app] AutoSSL: " + strconv.Itoa(copied) + " file(s) of the first engine's " + firstEnginesFolder + "/ (its certificates and its account key) copied to " +
		certificatesFolder + "/, read from now on; " + firstEnginesFolder + "/ is left as it was")
}

// copyFile writes what from holds to to, made for the owner alone (0600) as autocert writes its files; a file there
// is never written over.
func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		_ = os.Remove(to)
		return err
	}
	return target.Close()
}
