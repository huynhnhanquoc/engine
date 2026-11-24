package host

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// The times a request may hold the server, by default (Host's fields set others).
const (
	// defaultRequestTimeout bounds one request's run: energy counted instructions, and one instruction
	// (reverse, sort, join of an array of a million items) took a millisecond, so a request within its
	// energy ran for minutes (05/11/2026). Since 15/11/2026 energy counts the engine's work too (core/runtime,
	// work.go), some 44 ms a request: the time bounds what a run waits for (a call out, a database) and a
	// host's RunEnergy past it.
	defaultRequestTimeout = 30 * time.Second
	// defaultReadTimeout bounds the reading of a request, its headers and its body: a body announced and
	// never sent held a goroutine and the handler waiting on it for as long as the client liked.
	defaultReadTimeout = 30 * time.Second
	// readHeaderTimeout bounds the headers alone (docs/KITWORK_ROUTER.md, "Nhóm an toàn": 10 seconds).
	readHeaderTimeout = 10 * time.Second
	// idleTimeout closes a kept-alive connection that sends no next request.
	idleTimeout = 2 * time.Minute
)

// Serve answers the sites' requests until a server stops, then closes the others. Boot comes first: the
// sites are all open before the first request. Without allowLocal and unless tls: false it serves HTTPS
// as well, AutoSSL on 443 with port 80 for Let's Encrypt's challenge (TLSServers); the server on
// app.website({ port }) stays, plain HTTP, for the host's own calls (http://127.0.0.1:<port>), and listens
// on 127.0.0.1 alone (Server): a host has one public door.
func (h *Host) Serve() error {
	if !h.Manifest.Run {
		return errors.New("the manifest has no app.run(): it is read and checked, and nothing opens")
	}
	if h.Manifest.Website == nil && h.Manifest.Database != nil {
		// the database alone, as other SQL databases run (Quốc, 07/11/2026): its port, no website
		return h.ServeDatabase()
	}
	server, err := h.Server()
	if err != nil {
		return err
	}
	// app.database's port, before the sites: a site may read what a tool writes through it
	if _, err := h.DatabasePort(); err != nil {
		return err
	}
	// the apps' queues and schedules run while the host serves (background.go)
	h.StartBackground()
	servers := []*http.Server{server}
	closed := 0
	sites := h.current()
	for _, site := range sites {
		if site.closed != nil {
			closed++
		}
	}
	line := fmt.Sprintf("[app] serving %d sites on %s", len(sites)-closed, server.Addr)
	if secure, challenge := h.TLSServers(); secure != nil {
		servers = append(servers, secure, challenge)
		line = fmt.Sprintf("[app] serving %d sites over HTTPS on %s (AutoSSL, certificates in %s/), Let's Encrypt's challenge on %s, plain HTTP on %s for this machine alone",
			len(sites)-closed, secure.Addr, certificatesFolder, challenge.Addr, server.Addr)
	}
	if closed > 0 {
		line += fmt.Sprintf(" (%d more answer 503: their app did not boot)", closed)
	}
	h.log.Line(line)
	if h.oneApp && len(sites) > closed && h.Manifest.Website.AllowLocal {
		h.log.Line(h.aloneAddresses(sites))
	}
	h.StartWatching() // app.hotReload(true): an app whose files change boots again (reload.go)
	stopped := make(chan error, len(servers))
	for _, each := range servers {
		go func() {
			if each.TLSConfig != nil {
				stopped <- each.ListenAndServeTLS("", "")
				return
			}
			stopped <- each.ListenAndServe()
		}()
	}
	select {
	case err = <-stopped:
	case <-h.stopRequested():
		h.shutDown(servers) // Ctrl+C or SIGTERM (shutdown.go): no new connection, the requests going on end
	}
	for _, each := range servers {
		each.Close()
	}
	// the database files the apps' statements opened, so kitwork migrate may write them after
	return errors.Join(err, h.Close())
}

// ServeDatabase is kitwork database (Quốc, 08/11/2026): app.database's port alone, whatever else the manifest declares,
// the database run as other SQL databases run. Boot need not come first: without it no site opens and no app's code
// runs, and the port finds an app's database by the folders under app.root (appRootOf). It returns when the port
// stops, the files it opened closed. A server of the same manifest takes the same port and the same files: run one or
// the other.
func (h *Host) ServeDatabase() error {
	if err := h.Check(); err != nil {
		return err
	}
	if !h.Manifest.Run {
		return errors.New("the manifest has no app.run(): it is read and checked, and nothing opens")
	}
	if h.withoutManifest {
		return fmt.Errorf("no %s in %s: the app runs alone and opens no database port; app.database(…) in an application.work beside module.work opens one", filepath.Base(h.File), h.Folder)
	}
	if h.Manifest.Database == nil {
		return fmt.Errorf("%s declares no app.database(…): kitwork database has no port to open", filepath.Base(h.File))
	}
	if err := h.openLog(); err != nil {
		return err
	}
	address, err := h.DatabasePort()
	if err != nil {
		return err
	}
	line := "[app] the database runs alone on " + address.String()
	if h.Manifest.Website != nil {
		line += ", the website not opened"
	} else {
		line += ", no website"
	}
	h.log.Line(line)
	select {
	case <-h.port.done:
	case <-h.stopRequested():
		// Ctrl+C or SIGTERM (shutdown.go): Close closes the port, a statement going on is canceled, then the files
		h.log.Line("[app] stopping: the database's files close")
	}
	return h.Close()
}

// TLSServers are the two servers AutoSSL adds to Server (certificates.go): HTTPS on 443 with a certificate
// for each site, and port 80, which answers Let's Encrypt's http-01 challenge and sends every other
// request to https. Both are nil when the website serves no HTTPS: with allowLocal, or tls: false. The
// ports are the protocol's: Let's Encrypt checks a name on 80 and 443, never another port.
func (h *Host) TLSServers() (secure, challenge *http.Server) {
	if !h.autoSSL() || h.Sites() == nil {
		return nil, nil
	}
	h.adoptFirstEngines() // the first engine's certificates, the first time engine2 serves HTTPS here
	certificates := newCertificates(h, h.Manifest.Website.TLS.Email, h.CertificateAuthority)
	secure = &http.Server{
		Addr:              ":443",
		Handler:           h,
		TLSConfig:         certificates.TLSConfig(),
		ReadHeaderTimeout: min(readHeaderTimeout, h.readTimeout()),
		ReadTimeout:       h.readTimeout(),
		IdleTimeout:       idleTimeout,
	}
	challenge = &http.Server{
		Addr:              ":80",
		Handler:           certificates.ChallengeHandler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	return secure, challenge
}

// strictTransport is the HSTS every answer of AutoSSL's HTTPS carries (the owner, 14/11/2026): a browser that read it
// goes to the hostname over HTTPS alone for a day, writing https:// itself before anything leaves the machine, so the
// first request of a visit, http://shop.vn typed, is no longer plain text a network between can answer in the site's
// place (SSL stripping); port 80's redirect was that request's only guard. A day to begin, a year once it has served
// without trouble: a browser keeps it its whole time, the host cannot take it back, and a site whose certificate fails
// in that time (a renewal refused, Let's Encrypt's weekly ceiling) is refused by the browser with no way past, as is a
// tenant's hostname moved to a server of no HTTPS. Neither includeSubDomains, a promise for every name under a tenant's,
// nor preload, months to undo: a site that wants them writes its own, context.header("Strict-Transport-Security", …),
// which replaces this one. Never over plain HTTP, which browsers ignore it on, nor with allowLocal or tls: false, a
// host whose HTTPS is not Kitwork's to promise.
const strictTransport = "max-age=86400"

// autoSSL says the website serves HTTPS itself, AutoSSL: neither allowLocal nor tls: false.
func (h *Host) autoSSL() bool {
	website := h.Manifest.Website
	return website != nil && !website.AllowLocal && website.TLS.Automatic
}

// Server is the HTTP server Serve runs, on app.website({ port }), for a host that listens itself: the
// Host as its handler, and the times a request may hold it.
//
// A host has one public door (the owner, 14/11/2026). With AutoSSL it is HTTPS, 443, and port 80 sends to it; the
// plain port then listens on 127.0.0.1 alone, for the host's own calls, as app.database's port does: on every address
// it served every site without TLS to whoever wrote http://<the server's address>:<port>, a login's cookie and a form
// in plain text beside the HTTPS that guards them (the first engine did so, engine/kitwork.go). No option opens it: a
// host whose TLS ends before Kitwork writes tls: false, and the plain port is then its door, on every address, as
// with allowLocal on a machine of development.
func (h *Host) Server() (*http.Server, error) {
	if h.Manifest.Website == nil {
		return nil, errors.New("the manifest declares no website: app.website(…) is what Serve serves")
	}
	if h.Manifest.Website.Port == 0 {
		return nil, errors.New("the website has no port: app.website({ hostname, port: 8080 }) says it")
	}
	if h.Sites() == nil {
		return nil, errors.New("Serve before Boot: the sites are opened by Boot")
	}
	address := ":" + strconv.Itoa(h.Manifest.Website.Port)
	if h.autoSSL() || h.withoutManifest {
		address = "127.0.0.1" + address // an app run alone with no application.work: this machine alone (alone.go)
	}
	return &http.Server{
		Addr:              address,
		Handler:           h,
		ReadHeaderTimeout: min(readHeaderTimeout, h.readTimeout()),
		ReadTimeout:       h.readTimeout(),
		IdleTimeout:       idleTimeout,
	}, nil
}

// readTimeout is ReadTimeout, or its default.
func (h *Host) readTimeout() time.Duration {
	if h.ReadTimeout > 0 {
		return h.ReadTimeout
	}
	return defaultReadTimeout
}

// siteRequests is SiteRequests, or its default: twice the processors, at least 8. A site's requests wait on the
// network or a file as often as they compute, so a site may run more than the processors; twice keeps one site's from
// crowding out the others when they all compute.
func (h *Host) siteRequests() int {
	if h.SiteRequests > 0 {
		return h.SiteRequests
	}
	return max(8, 2*goruntime.GOMAXPROCS(0))
}

// runBytes is RunBytes, or its default.
func (h *Host) runBytes() uint64 {
	if h.RunBytes > 0 {
		return h.RunBytes
	}
	return madeBytes
}

// runEnergy is RunEnergy, or its default.
func (h *Host) runEnergy() uint64 {
	if h.RunEnergy > 0 {
		return h.RunEnergy
	}
	return energy
}

// siteWait is SiteWait, or its default.
func (h *Host) siteWait() time.Duration {
	if h.SiteWait > 0 {
		return h.SiteWait
	}
	return defaultSiteWait
}

// requestTimeout is RequestTimeout, or its default.
func (h *Host) requestTimeout() time.Duration {
	if h.RequestTimeout > 0 {
		return h.RequestTimeout
	}
	return defaultRequestTimeout
}

// ServeHTTP gives a request to the site its Host names. A panic of the engine is a 500 and a line in
// the log, never a host down: one site's request must not stop the others'.
func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS != nil && h.autoSSL() {
		w.Header().Set("Strict-Transport-Security", strictTransport) // HSTS; a site's own header(…) replaces it
	}
	// gzipped for a browser that takes it (compress.go); ended after the panic's 500 is written
	w, finish := compressed(w, r)
	defer finish()
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			h.log.Error("[app] ", fmt.Sprintf("%s %s %s: panic: %v\n%s", visitorText(r.Host), visitorText(r.Method), visitorText(r.URL.Path), recovered, debug.Stack()))
			answer(w, http.StatusInternalServerError, "Internal Server Error")
		}
	}()
	if h.limited(w, r) { // the host's ceilings, before any site is looked for (ratelimit.go)
		return
	}
	if h.redirected(w, r) { // a hostname the table of domains sends elsewhere (domains.go)
		return
	}
	site := h.site(r.Host)
	if site == nil {
		answer(w, http.StatusNotFound, "Not Found")
		return
	}
	site.serve(w, r, h.requestTimeout())
}

// site is the site host names: in lower case, without its port or a final dot, the name of a site's
// folder. With allowLocal, <domain>.localhost is <domain>, and the machine itself (localhost, 127.0.0.1,
// ::1) is the website's hostname. A host no folder names is no site: nothing is opened on demand.
func (h *Host) site(host string) *Site {
	host = requestHostname(host)
	sites := h.current() // read once: the request runs on one boot of its app (reload.go)
	if site, ok := sites[host]; ok {
		return site
	}
	website := h.Manifest.Website
	if website == nil || !website.AllowLocal {
		return nil
	}
	if name, ok := strings.CutSuffix(host, ".localhost"); ok {
		return sites[name]
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		if site := sites[website.Hostname]; site != nil {
			return site
		}
		return h.onlySite(sites) // an app run alone answers the machine with its one site (alone.go)
	}
	return nil
}
