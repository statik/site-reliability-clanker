package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/statik/site-reliability-clanker/internal/setup"
)

// runSetup serves the first-run wizard on the loopback address and opens it in the
// browser. For the local target it also runs the control plane, in this process.
func runSetup(ctx context.Context, o options) error {
	host, _, err := net.SplitHostPort(o.setupAddr)
	if err != nil {
		return fmt.Errorf("read --setup-addr %q: %w", o.setupAddr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--setup-addr must be a loopback address such as 127.0.0.1:7069: "+
			"the wizard acts with your cloud sign-in, so it does not listen on %s", host)
	}
	if o.inApp {
		ctx = stopWithParent(ctx)
	}
	// A released binary unpacks only the platform files: the installer on AWS then needs
	// a checkout, which the person gives in the wizard.
	checkout := ""
	if _, err := os.Stat(filepath.Join(o.sourceDir, "build", "runner", "Dockerfile")); err == nil {
		checkout = o.sourceDir
	}
	wizard, err := setup.New(ctx, setup.Config{
		DataDir: o.dataDir, Version: version, DashboardURL: "http://" + o.addr,
		InApp: o.inApp, Application: o.app.Title,
		Layers: awsLayers{o: o, run: runAWS}, Checkout: checkout,
		StartLocal: func(ctx context.Context, env, operator string) error {
			local := o
			local.env, local.cloud, local.demo = env, "local", true
			local.operator = operator
			go func() {
				if err := up(ctx, local); err != nil {
					slog.Error("control plane stopped", "error", err)
				}
			}()
			return nil
		},
	})
	if err != nil {
		return err
	}
	handler, err := wizard.Handler(o.setupAddr)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", o.setupAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w; another `clanker setup` may run, or set "+
			"--setup-addr", o.setupAddr, err)
	}
	address := fmt.Sprintf("http://%s/?token=%s", o.setupAddr, wizard.Token())
	fmt.Printf("The setup wizard runs. Open this address in your browser:\n\n  %s\n\n"+
		"Press Ctrl+C to stop.\n", address)
	if !o.noBrowser && !o.inApp {
		openBrowser(address)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("stop the wizard", "error", err)
		}
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("wizard on %s: %w", o.setupAddr, err)
	}
	return nil
}

// stopWithParent returns a context that ends when the program that started this one is
// gone. The macOS app is that program: after a forced quit of the app, the wizard and the
// control plane must not stay and hold their ports.
func stopWithParent(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	parent := os.Getppid()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if os.Getppid() != parent {
					slog.Warn("the app that started the wizard is gone; stopping")
					cancel()
					return
				}
			}
		}
	}()
	return ctx
}

// openBrowser shows the address in the default browser. A failure is not an error: the
// address is printed, and the person can open it by hand.
func openBrowser(address string) {
	program, args := "xdg-open", []string{address}
	switch runtime.GOOS {
	case "darwin":
		program = "open"
	case "windows":
		program, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	}
	if err := exec.Command(program, args...).Start(); err != nil {
		slog.Warn("could not open the browser; open the address by hand", "error", err)
	}
}
