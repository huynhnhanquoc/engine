package application

// Website is app.website(…): the host serves the sites of its apps over HTTP. Hostname is the site a
// request to the machine itself reaches when AllowLocal is on (localhost, 127.0.0.1); every site is still
// reached by its own folder's name.
type Website struct {
	Hostname string
	// Port is port: the host's plain HTTP server (http://127.0.0.1:<port>), beside AutoSSL's 443 and 80, on 127.0.0.1
	// alone with AutoSSL and on every address without it (host Server); 0 when the manifest writes none, which
	// app.run() refuses (host Check)
	Port int
	// AllowLocal is allowLocal: the host may be reached as localhost, and <domain>.localhost reaches the
	// site <domain>, for local development.
	AllowLocal bool
	// TLS is tls: AutoSSL unless the manifest wrote tls: false (tls.go).
	TLS TLS
	// RateLimit is rateLimit: the host's ceilings of requests (limits.go); nil for none.
	RateLimit *RateLimit
	// Domains is domains: the SQLite file of the host's table of domains, which app serves each hostname
	// (host/domains.go), read from the manifest's folder; "" when the manifest writes none, and the table is the host's
	// .database/system/kitwork.db, needed when a site under apps/ has a public name: one not there stops the boot.
	Domains string
}
