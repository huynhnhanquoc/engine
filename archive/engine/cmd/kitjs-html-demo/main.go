// kitjs-html-demo serves the isolated browser-side HTML binding prototype.
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	kitjs "github.com/kitwork/engine/jit/js"
)

//go:embed index.html demo.js
var demo embed.FS

func main() {
	address := flag.String("addr", "127.0.0.1:0", "local HTTP address (port 0 selects a free port)")
	flag.Parse()
	page, err := demo.ReadFile("index.html")
	if err != nil {
		log.Fatal(err)
	}
	pageHTML := kitjs.Deliver(string(page), kitjs.Options{})
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'none'; object-src 'none'; base-uri 'none'; frame-src 'none'")
		if asset, exact := kitjs.AssetAt(r.URL.Path); exact && asset != nil {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(asset.Body)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTML))
		case "/demo.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			source, _ := demo.ReadFile("demo.js")
			_, _ = w.Write(source)
		default:
			http.NotFound(w, r)
		}
	})
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("KitJS HTML prototype: http://%s\n", listener.Addr())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.Serve(listener))
}
