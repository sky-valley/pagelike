// Command pagelike runs and administers a pagelike instance.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/jsglue"
	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/reactions"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

const usage = `pagelike — a PageLove-compatible document runtime

Usage:
  pagelike serve   [--data DIR] [--listen ADDR] [--domain DOMAIN] [--trust-proxy] [--dev-insecure-auth]
                   [--outbound-allow-private] [--outbound-allow LIST]
  pagelike host    [--data DIR] [--listen 127.0.0.1:8788] (managed hosting; see docs/hosting.md)
  pagelike site    create NAME [--default-get allow|deny] | list | delete NAME
  pagelike key     create [--site NAME|*] [--label TEXT] [--ttl 720h] | list | revoke ID
` + identityUsage + `  pagelike export  --site NAME --out DIR
  pagelike import  --site NAME --in DIR [--create]
  pagelike fork    --from SRC --to DST [--note TEXT]
  pagelike backup  --out DIR
  pagelike migrate --from-dav URL --out DIR [--key-file F] [--import --site NAME]
  pagelike events  prune --site NAME [--older-than 10m]   (drop stored stream events;
                   clients reconnecting from before then get a reset)
  pagelike version
  pagelike jsrt-worker    (internal: a server-JavaScript worker on stdin/stdout)

Common flag: --data DIR (default ./data, or $PAGELIKE_DATA).
`

func main() {
	// Server JavaScript runs in worker processes started from this binary
	// (docs/decisions/0001-server-js-engine.md); become one if asked to.
	jsrt.MaybeRunWorker()
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "host":
		err = cmdHost(os.Args[2:])
	case "site":
		err = cmdSite(os.Args[2:])
	case "key":
		err = cmdKey(os.Args[2:])
	case "user":
		err = cmdUser(os.Args[2:])
	case "identity":
		err = cmdIdentity(os.Args[2:])
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "export", "import", "fork", "backup":
		err = cmdPortable(os.Args[1], os.Args[2:])
	case "events":
		err = cmdEvents(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("pagelike", buildVersion())
		return
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pagelike:", err)
		os.Exit(1)
	}
}

func defaultData() string {
	if v := os.Getenv("PAGELIKE_DATA"); v != "" {
		return v
	}
	return "./data"
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	listen := fs.String("listen", "127.0.0.1:8787", "listen address")
	domain := fs.String("domain", "localhost", "base domain (sites at <site>.<domain>)")
	trust := fs.Bool("trust-proxy", false, "trust X-Forwarded-Proto from a TLS-terminating proxy")
	dev := fs.Bool("dev-insecure-auth", false, "DEVELOPMENT ONLY: accept X-Pagelike-Dev-User impersonation from loopback clients")
	allowPrivate := fs.Bool("outbound-allow-private", false, "let reactions send outbound HTTP requests to private and loopback addresses")
	allowOut := fs.String("outbound-allow", "", "comma-separated private destinations reactions may reach (host, ip, host:port, CIDR)")
	jsWorkers := fs.Int("js-workers", 0, "server JavaScript worker processes (0 = one per CPU); started on first use")
	fs.Parse(args)
	jsglue.SetWorkers(*jsWorkers)
	defer jsglue.Close()
	if err := server.CheckListen(*listen, *dev); err != nil {
		return err
	}
	var allow []string
	for _, a := range strings.Split(*allowOut, ",") {
		if a = strings.TrimSpace(a); a != "" {
			allow = append(allow, a)
		}
	}
	reactions.SetOutboundPolicy(*allowPrivate, allow)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reg, err := site.NewRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	ctl, err := control.Open(*data)
	if err != nil {
		return err
	}
	defer ctl.Close()
	srv := server.New(server.Config{Domain: *domain, TrustProxy: *trust, DevAuth: *dev}, reg, ctl, log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.StartJanitor(ctx)
	hs := &http.Server{Addr: *listen, Handler: srv, ReadHeaderTimeout: 15 * time.Second}
	if *dev {
		log.Warn("DEVELOPMENT AUTH BYPASS ENABLED: X-Pagelike-Dev-User is honoured from loopback clients")
	}
	log.Info("pagelike listening", "version", buildVersion(), "addr", *listen, "sites", "<site>."+*domain, "authoring", "dav-<site>."+*domain, "data", *data)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sctx)
	}()
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openRegistry(data string) (*site.Registry, error) { return site.NewRegistry(data) }

func cmdSite(args []string) error {
	if len(args) < 1 {
		return errors.New("site: expected create|list|delete")
	}
	fs := flag.NewFlagSet("site", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	dg := fs.String("default-get", "allow", "default-GET mode: allow|deny")
	sub := args[0]
	fs.Parse(reorder(args[1:]))
	reg, err := openRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	ctx := context.Background()
	switch sub {
	case "create":
		if fs.NArg() != 1 {
			return errors.New("site create NAME")
		}
		if _, err := reg.Create(ctx, fs.Arg(0), site.Settings{DefaultGet: *dg}); err != nil {
			return err
		}
		fmt.Printf("created site %s\n", fs.Arg(0))
	case "list":
		names, err := reg.List()
		if err != nil {
			return err
		}
		for _, n := range names {
			fmt.Println(n)
		}
	case "delete":
		if fs.NArg() != 1 {
			return errors.New("site delete NAME")
		}
		return reg.Delete(fs.Arg(0))
	default:
		return fmt.Errorf("site: unknown subcommand %q", sub)
	}
	return nil
}

func cmdKey(args []string) error {
	if len(args) < 1 {
		return errors.New("key: expected create|list|revoke")
	}
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	siteName := fs.String("site", "*", "site the key may author (* = all, instance admin)")
	label := fs.String("label", "", "label")
	ttl := fs.Duration("ttl", 0, "lifetime (0 = no expiry)")
	sub := args[0]
	fs.Parse(reorder(args[1:]))
	if err := os.MkdirAll(*data, 0o750); err != nil {
		return err
	}
	ctl, err := control.Open(*data)
	if err != nil {
		return err
	}
	defer ctl.Close()
	ctx := context.Background()
	switch sub {
	case "create":
		secret, k, err := ctl.CreateKey(ctx, *label, strings.Split(*siteName, ","), *ttl)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "created key %s (prefix %s) for sites %s — shown once:\n", k.ID, k.Prefix, *siteName)
		fmt.Println(secret)
	case "list":
		keys, err := ctl.List(ctx)
		if err != nil {
			return err
		}
		for _, k := range keys {
			exp := "never"
			if k.ExpiresMS != 0 {
				exp = time.UnixMilli(k.ExpiresMS).UTC().Format(time.RFC3339)
			}
			fmt.Printf("%s\t%s…\t%s\t%s\texpires %s\n", k.ID, k.Prefix, strings.Join(k.Sites, ","), k.Label, exp)
		}
	case "revoke":
		if fs.NArg() != 1 {
			return errors.New("key revoke ID")
		}
		ok, err := ctl.Revoke(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("no such key")
		}
	default:
		return fmt.Errorf("key: unknown subcommand %q", sub)
	}
	return nil
}

// reorder moves flags before positional arguments so "site create foo --x"
// works with the standard flag package.
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !isBoolFlag(a) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func isBoolFlag(a string) bool {
	switch strings.TrimLeft(a, "-") {
	case "verified", "password-stdin", "create", "trust-proxy", "dev-insecure-auth", "authored-from-live", "clear", "client-secret-stdin", "import":
		return true
	}
	return false
}

func cmdPortable(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	siteName := fs.String("site", "", "site")
	out := fs.String("out", "", "output directory")
	in := fs.String("in", "", "input directory")
	state := fs.String("state", "live", "export: live (current documents + authored baseline) or authored")
	create := fs.Bool("create", false, "import: create the site")
	authoredFromLive := fs.Bool("authored-from-live", false, "import: treat imported documents as the authored baseline")
	from := fs.String("from", "", "fork: source site")
	to := fs.String("to", "", "fork: new site")
	note := fs.String("note", "", "fork: note recorded in lineage")
	fs.Parse(reorder(args))
	reg, err := openRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	ctx := context.Background()
	switch cmd {
	case "export":
		st, err := reg.Get(ctx, *siteName)
		if err != nil || *out == "" {
			return fmt.Errorf("export --site NAME --out DIR (%v)", err)
		}
		m, err := site.Export(ctx, st, *out, *state)
		if err != nil {
			return err
		}
		fmt.Printf("exported %d documents (%s state, version %s) to %s\n", len(m.Documents), m.State, m.Version, *out)
	case "import":
		if *in == "" || *siteName == "" {
			return errors.New("import --site NAME --in DIR [--create]")
		}
		var st *site.Site
		if *create {
			st, err = reg.Create(ctx, *siteName, site.Settings{})
		} else {
			st, err = reg.Get(ctx, *siteName)
		}
		if err != nil {
			return err
		}
		m, err := site.Import(ctx, st, *in, site.ImportOptions{AuthoredFromLive: *authoredFromLive})
		if err != nil {
			return err
		}
		fmt.Printf("imported %d documents into %s; not imported: %s\n", len(m.Documents), *siteName, strings.Join(m.NotExported, "; "))
	case "fork":
		if *from == "" || *to == "" {
			return errors.New("fork --from SRC --to DST")
		}
		st, err := site.Fork(ctx, reg, *from, *to, *note)
		if err != nil {
			return err
		}
		l := st.Settings().Lineage
		fmt.Printf("forked %s → %s (source version %s); participant state, identities and keys were not copied\n", *from, *to, l.SourceDigest)
	case "backup":
		if *out == "" {
			return errors.New("backup --out DIR")
		}
		return backup(ctx, reg, *data, *out)
	}
	return nil
}

// backup writes a consistent copy of every site database (VACUUM INTO),
// its blobs, and the control database. Restore by stopping pagelike and
// copying the backup directory over the data directory.
func backup(ctx context.Context, reg *site.Registry, data, out string) error {
	names, err := reg.List()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "sites"), 0o750); err != nil {
		return err
	}
	for _, n := range names {
		st, err := reg.Get(ctx, n)
		if err != nil {
			return err
		}
		dst := filepath.Join(out, "sites", n)
		if err := os.MkdirAll(filepath.Join(dst, "blobs"), 0o750); err != nil {
			return err
		}
		os.Remove(filepath.Join(dst, "site.db"))
		if err := st.Store.Backup(ctx, filepath.Join(dst, "site.db")); err != nil {
			return fmt.Errorf("backup %s: %w", n, err)
		}
		entries, _ := os.ReadDir(filepath.Join(st.Store.Dir(), "blobs"))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if err := copyFile(filepath.Join(st.Store.Dir(), "blobs", e.Name()), filepath.Join(dst, "blobs", e.Name())); err != nil {
				return err
			}
		}
		fmt.Printf("backed up site %s\n", n)
	}
	ctl, err := control.Open(data)
	if err != nil {
		return err
	}
	defer ctl.Close()
	os.Remove(filepath.Join(out, "control.db"))
	return ctl.Backup(ctx, filepath.Join(out, "control.db"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	o, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}

// cmdEvents maintains a site's stored stream events. The server prunes
// events older than the retention window whenever a client reconnects;
// "prune" does the same on demand, with any age (0 drops them all).
func cmdEvents(args []string) error {
	if len(args) < 1 || args[0] != "prune" {
		return errors.New("events: expected prune --site NAME [--older-than DURATION]")
	}
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	siteName := fs.String("site", "", "site")
	older := fs.Duration("older-than", 10*time.Minute, "drop events older than this (0 = all)")
	fs.Parse(reorder(args[1:]))
	if *siteName == "" {
		return errors.New("events prune: --site is required")
	}
	reg, err := openRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	ctx := context.Background()
	s, err := reg.Get(ctx, *siteName)
	if err != nil {
		return err
	}
	// An age of 0 must include events stamped in the current millisecond.
	n, err := s.Store.PruneEvents(ctx, time.Now().Add(-*older).Add(time.Millisecond))
	if err != nil {
		return err
	}
	fmt.Printf("pruned %d events from %s\n", n, *siteName)
	return nil
}

// version is set at release build time (-ldflags "-X main.version=v1.2.3").
var version = ""

// buildVersion reports the release version, or the module version for
// `go install …@version` builds, or "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
