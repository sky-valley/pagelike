package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sky-valley/pagelike/internal/hosting"
	"github.com/sky-valley/pagelike/internal/jsglue"
	"github.com/sky-valley/pagelike/internal/reactions"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

func cmdHost(args []string) error {
	fs := flag.NewFlagSet("host", flag.ContinueOnError)
	data := fs.String("data", defaultData(), "durable data directory")
	listen := fs.String("listen", "127.0.0.1:8788", "loopback listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := server.CheckListen(*listen, true); err != nil {
		return errors.New("host requires a loopback listener behind an authenticated TLS edge")
	}
	reg, err := site.NewRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := hosting.Config{Domain: os.Getenv("PAGELIKE_DOMAIN"), AdminToken: os.Getenv("PAGELIKE_ADMIN_TOKEN"), EdgeToken: os.Getenv("PAGELIKE_EDGE_TOKEN"), IdentityURL: os.Getenv("PAGELIKE_IDENTITY_URL"), IdentityToken: os.Getenv("PAGELIKE_IDENTITY_TOKEN"), FrameOrigins: strings.Fields(os.Getenv("PAGELIKE_FRAME_ORIGINS"))}
	core := server.New(server.Config{Domain: cfg.Domain, TrustProxy: true}, reg, nil, log)
	handler, err := hosting.New(cfg, core)
	if err != nil {
		return err
	}
	jsglue.SetWorkers(2)
	defer jsglue.Close()
	reactions.SetOutboundPolicy(false, nil)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	core.StartJanitor(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := handler.PruneTemporary(ctx, time.Now()); err != nil {
					log.Warn("temporary site cleanup incomplete")
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	hs := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(deadline)
	}()
	log.Info("managed document runtime listening", "version", buildVersion(), "addr", *listen)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
