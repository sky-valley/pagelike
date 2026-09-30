package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// cmdServe runs a small static-file preview server over a previously
// built outDir. The remap from .md Accept-negotiation is intentionally
// local-only; the published Pages deployment serves .md directly.
func cmdServe(args []string) error {
	outDir := "site/public"
	addr := "127.0.0.1:9000"
	fs := flagSet("serve")
	fs.StringVar(&outDir, "out", outDir, "directory of a previously built site")
	fs.StringVar(&addr, "addr", addr, "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("site serve: refusing to bind on non-loopback address %q", host)
	}
	fsrv := http.FileServer(http.Dir(outDir))
	mux := http.NewServeMux()
	mux.HandleFunc("/llms.txt", serveBare(outDir, "llms.txt"))
	mux.HandleFunc("/llms-full.txt", serveBare(outDir, "llms-full.txt"))
	mux.HandleFunc("/sitemap.xml", serveBare(outDir, "sitemap.xml"))
	mux.HandleFunc("/robots.txt", serveBare(outDir, "robots.txt"))
	mux.HandleFunc("/index.json", serveBare(outDir, "index.json"))
	mux.HandleFunc("/", negotiation(outDir, fsrv))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "site serve: %s serving %s\n", "http://"+net.JoinHostPort(host, port), outDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// serveBare returns a handler that sends a single file, useful because
// the FileServer's directory listing can otherwise intercept /-less
// requests under some directory layouts.
func serveBare(outDir, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path.Join(outDir, name)
		http.ServeFile(w, r, p)
	}
}

// negotiation returns a wrapper around the directory FileServer that
// serves an .md variant when the request carries Accept: text/markdown
// or type/markdown. The real Pages deployment serves the .md files
// directly through GitHub Pages' content-type rules; this is just for
// local browser preview.
func negotiation(outDir string, fsrv http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if acceptsMarkdown(r.Header.Get("Accept")) {
			if md := mdPathFor(outDir, r.URL.Path); md != "" {
				w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
				http.ServeFile(w, r, md)
				return
			}
		}
		fsrv.ServeHTTP(w, r)
	}
}

func acceptsMarkdown(accept string) bool {
	if accept == "" {
		return false
	}
	for _, piece := range strings.Split(accept, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "text/markdown" || piece == "type/markdown" {
			return true
		}
		if strings.HasPrefix(piece, "text/markdown;") || strings.HasPrefix(piece, "type/markdown;") {
			return true
		}
	}
	return false
}

func mdPathFor(outDir, urlPath string) string {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		return path.Join(outDir, "index.md")
	}
	candidate := path.Join(outDir, clean, "index.md")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
