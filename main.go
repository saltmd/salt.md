package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"salt/server"
)

//go:embed all:web/dist
var distFS embed.FS

// The notices travel with the binary, not only with the repository. Somebody
// who installs salt.md with one command never sees GitHub, and "the notice
// accompanies what you ship" is the one duty almost every licence here imposes.
//
//go:embed THIRD-PARTY-NOTICES.md
var noticesMD string

func main() {
	dataDir := server.EnvOr("DATA", "./data")

	// CLI subcommands for operations (backup/restore). No server needed.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "backup":
			dest := "salt-backup.tar.gz"
			if len(os.Args) > 2 {
				dest = os.Args[2]
			}
			if err := server.Backup(dataDir, dest); err != nil {
				log.Fatalf("backup: %v", err)
			}
			fmt.Printf("Backup written to %s\n", dest)
			return
		case "restore":
			if len(os.Args) < 3 {
				log.Fatal("usage: salt restore <backup.tar.gz>")
			}
			if err := server.Restore(dataDir, os.Args[2]); err != nil {
				log.Fatalf("restore: %v", err)
			}
			fmt.Printf("Restored %s into %s\n", os.Args[2], dataDir)
			return
		case "version":
			fmt.Println(server.Version)
			return
		case "fix-notion-rows":
			// One-time: strip Notion's repeated "# title + Property: value"
			// preamble from existing database-row bodies (run with the server
			// stopped — it takes the sole SQLite connection).
			n, err := server.FixNotionRows(dataDir)
			if err != nil {
				log.Fatalf("fix-notion-rows: %v", err)
			}
			fmt.Printf("Cleaned %d row bodies\n", n)
			return
		}
	}

	addr := server.EnvOr("ADDR", ":8420")
	certFile := server.Env("TLS_CERT")
	keyFile := server.Env("TLS_KEY")

	dist, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatalf("embedded frontend missing: %v", err)
	}

	srv, err := server.New(dataDir, dist)
	if err == nil {
		srv.SetNotices(noticesMD)
	}
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	srv.SetAddr(addr)
	// A persistent Cloudflare tunnel enabled in the settings comes back up on
	// its own after every restart — public access is part of the product.
	srv.AutostartTunnel()

	httpSrv := &http.Server{Addr: addr, Handler: srv}

	// Serve in the background; the main goroutine waits for a termination signal
	// so we can drain in-flight requests and checkpoint the DB before exiting.
	serveErr := make(chan error, 1)
	go func() {
		// Built-in HTTPS (admin setting): Let's Encrypt via autocert — no
		// reverse proxy needed. Requires ports 80+443 and public DNS.
		if domain, enabled := srv.PublicHTTPSConfig(); enabled && domain != "" && certFile == "" {
			m := &autocert.Manager{
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(domain),
				Cache:      autocert.DirCache(filepath.Join(dataDir, "certs")),
			}
			// :80 answers the ACME challenge and redirects everything else.
			go func() {
				if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
					log.Printf("http-01 listener: %v", err)
				}
			}()
			httpSrv.Addr = ":443"
			httpSrv.TLSConfig = m.TLSConfig()
			log.Printf("salt.md %s listening on :443 (auto-HTTPS for %s, data: %s)", server.Version, domain, dataDir)
			serveErr <- httpSrv.ListenAndServeTLS("", "")
			return
		}
		if certFile != "" && keyFile != "" {
			log.Printf("salt.md %s listening on %s (TLS, data: %s)", server.Version, addr, dataDir)
			serveErr <- httpSrv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Printf("salt.md %s listening on %s (data: %s)", server.Version, addr, dataDir)
			serveErr <- httpSrv.ListenAndServe()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case sig := <-stop:
		log.Printf("received %s, shutting down…", sig)
		// Cloudflare first: the edge has to hear that this connector is leaving,
		// otherwise it keeps routing to a dead process and the public domain is
		// down for minutes after every restart. Autostart stays enabled — this
		// is a restart, not the admin switching public access off.
		srv.SignalTunnelStop()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
		// Only collect now — the SIGTERM already ran during the drain. Together
		// the stop stays under systemd's TimeoutStopSec (20s).
		srv.AwaitTunnelStop(4 * time.Second)
		if err := srv.Close(); err != nil {
			log.Printf("db close: %v", err)
		}
		log.Print("stopped cleanly")
	}
}
