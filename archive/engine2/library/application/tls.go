package application

// TLS is app.website's tls option: how the website's HTTPS is had (docs/KITWORK_BOOT.md mục 3, luật 4: an
// option belongs to its surface). By default AutoSSL, with nothing written: the host answers HTTPS on 443
// with a certificate Let's Encrypt gives each site, asked for the first time the site is reached, as the
// first engine did. tls: false serves no HTTPS, for a host whose TLS ends before Kitwork; tls: { email }
// gives Let's Encrypt the address it writes to before a certificate expires. allowLocal serves no HTTPS
// whatever tls says: a machine of development has no certificate to ask for.
type TLS struct {
	// Automatic is AutoSSL: true unless the manifest wrote tls: false.
	Automatic bool
	// Email is tls: { email }, "" for none.
	Email string
}
