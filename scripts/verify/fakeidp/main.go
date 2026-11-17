// Command fakeidp runs the fake identity provider for the checks in scripts/verify. It
// signs in whoever a check names, with no password. Never use it for a real control plane.
//
// Usage: fakeidp --addr 127.0.0.1:7190 --issuer http://127.0.0.1:7190
//
// Choose the account of the next sign-in with POST /person?email=...&verified=true.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/statik/site-reliability-clanker/internal/signin/fakeidp"
)

func run() error {
	addr := flag.String("addr", "127.0.0.1:7190", "listen address")
	issuer := flag.String("issuer", "", "address of the provider as its clients see it")
	flag.Parse()
	if *issuer == "" {
		*issuer = "http://" + *addr
	}
	provider, err := fakeidp.New(*issuer)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *addr, Handler: provider.Handler(),
		ReadHeaderTimeout: 10 * time.Second}
	return server.ListenAndServe()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeidp:", err)
		os.Exit(1)
	}
}
