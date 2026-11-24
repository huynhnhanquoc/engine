package host_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kitwork/engine2/host"
)

// What one request costs the host, without the network: the Host looked up, the route found, a VM of the
// site taken and reset, the handler run, the answer written. A text route and a JSON route of a typed
// parameter.
func BenchmarkARequest(b *testing.B) {
	read, err := host.ReadWithLog(testApp, io.Discard)
	if err != nil {
		b.Fatal(err)
	}
	if err := read.Boot(); err != nil {
		b.Fatal(err)
	}
	for _, path := range []string{"/api?q=x", "/users/42", "/clock"} {
		b.Run(path, func(b *testing.B) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Host = "alpha.test"
			b.ReportAllocs()
			for b.Loop() {
				recorder := httptest.NewRecorder()
				read.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK {
					b.Fatalf("%s: %d %s", path, recorder.Code, recorder.Body)
				}
			}
		})
	}
}
