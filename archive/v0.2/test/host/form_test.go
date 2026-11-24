package host_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// request.form(name) is the first value of a field of the body, "" when it has none; it never reads the URL's
// query; a file of a multipart body is no field; a body that is no form, or a malformed one, has no field; a second
// read gives the same. request.form() is every field, each its first value, in the order they came. A body past
// the ceiling of a read answers 413 (docs/KITWORK_ROUTER.md mục 5).
func TestAFormIsReadFromTheBody(t *testing.T) {
	b, failed := bootClosing(t, map[string]string{
		"apps/shop/shop.test/router.work": routerImport + `router.post((context) => context.json({
  name: context.request.form("name"), missing: context.request.form("missing"), again: context.request.form("name"),
  all: context.request.form() }));
`,
	})
	if failed != "" {
		t.Fatal(failed)
	}
	var multi bytes.Buffer
	writer := multipart.NewWriter(&multi)
	writer.WriteField("name", "Cà phê")
	file, _ := writer.CreateFormFile("photo", "cup.png")
	file.Write([]byte("png bytes"))
	writer.WriteField("size", "l")
	writer.WriteField("name", "second")
	writer.Close()
	whole := multi.String()

	const urlencoded = "application/x-www-form-urlencoded"
	const empty = `{"name":"","missing":"","again":"","all":{}}`
	for _, c := range []struct {
		label, path, contentType, body string
		status                         int
		want                           string
	}{
		{"urlencoded", "/?name=query", urlencoded, "name=C%C3%A0+ph%C3%AA&size=l&name=second&photo=",
			200, `{"name":"Cà phê","missing":"","again":"Cà phê","all":{"name":"Cà phê","size":"l","photo":""}}`},
		{"urlencoded with its charset", "/", urlencoded + "; charset=utf-8", "name=a", 200, `{"name":"a","missing":"","again":"a","all":{"name":"a"}}`},
		{"multipart", "/", writer.FormDataContentType(), whole,
			200, `{"name":"Cà phê","missing":"","again":"Cà phê","all":{"name":"Cà phê","size":"l"}}`},
		{"the query only", "/?name=query", urlencoded, "size=l", 200, `{"name":"","missing":"","again":"","all":{"size":"l"}}`},
		{"JSON", "/", "application/json", `{"name":"a"}`, 200, empty},
		{"no Content-Type", "/", "", "name=a", 200, empty},
		{"a bad %", "/", urlencoded, "name=%zz", 200, empty},
		{"a ; in place of &", "/", urlencoded, "name=a;size=l", 200, empty},
		{"multipart without its boundary", "/", "multipart/form-data", whole, 200, empty},
		{"multipart parted by an empty boundary", "/", "multipart/form-data",
			"--\r\nContent-Disposition: form-data; name=\"name\"\r\n\r\nsneaky\r\n----\r\n", 200, empty},
		{"multipart cut short", "/", writer.FormDataContentType(), whole[:len(whole)/2], 200, empty},
		{"past the ceiling", "/", urlencoded, "name=" + strings.Repeat("x", 1<<20), 413, "Request Entity Too Large\n"},
	} {
		var headers []string
		if c.contentType != "" {
			headers = []string{"Content-Type", c.contentType}
		}
		got := b.request(t, http.MethodPost, "shop.test", c.path, strings.NewReader(c.body), headers...)
		if got.status != c.status || got.body != c.want {
			t.Errorf("%s: %d %s\nwant %d %s", c.label, got.status, got.body, c.status, c.want)
		}
	}
}
