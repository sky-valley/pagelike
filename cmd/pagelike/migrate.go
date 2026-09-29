package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sky-valley/pagelike/internal/migrate"
	"github.com/sky-valley/pagelike/internal/site"
)

// cmdMigrate copies a site from a WebDAV authoring mount (a PageLove host's
// webdav-url, or another pagelike) into an export directory and optionally
// imports it. The authoring key is read from an environment variable or a
// KEY=VALUE file, never from the command line.
func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	from := fs.String("from-dav", "", "source WebDAV URL, e.g. https://dav-host.onpagelove.com/")
	keyEnv := fs.String("key-env", "PAGELOVE_API_KEY", "environment variable holding the source authoring key")
	keyFile := fs.String("key-file", "", "KEY=VALUE file holding the key variable (e.g. .secrets/pagelove.env)")
	out := fs.String("out", "", "export directory to write")
	siteName := fs.String("site", "", "pagelike site to import into (with --import)")
	doImport := fs.Bool("import", false, "import the export into --site (created if missing)")
	defaultGet := fs.String("default-get", "allow", "source host default-GET mode to record")
	exclude := fs.String("exclude", "", "comma-separated path prefixes not to copy")
	fs.Parse(reorder(args))
	if *from == "" || *out == "" {
		return errors.New("migrate --from-dav URL --out DIR [--key-file F] [--import --site NAME]")
	}
	key := os.Getenv(*keyEnv)
	if *keyFile != "" {
		v, err := readKeyFile(*keyFile, *keyEnv)
		if err != nil {
			return err
		}
		key = v
	}
	if key == "" {
		return fmt.Errorf("no authoring key: set $%s or pass --key-file", *keyEnv)
	}
	var ex []string
	if *exclude != "" {
		ex = strings.Split(*exclude, ",")
	}
	src := &migrate.Source{URL: *from, BearerKey: key, DefaultGet: *defaultGet, Exclude: ex,
		Log: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }}
	ctx := context.Background()
	name := *siteName
	if name == "" {
		name = "migrated"
	}
	m, err := src.Export(ctx, *out, name)
	if err != nil {
		return err
	}
	fmt.Printf("exported %d files and %d collections from %s to %s\n", len(m.Documents), len(m.Dirs), *from, *out)
	fmt.Printf("not exported: %s\n", strings.Join(m.NotExported, "; "))
	if !*doImport {
		return nil
	}
	if *siteName == "" {
		return errors.New("--import needs --site")
	}
	reg, err := openRegistry(*data)
	if err != nil {
		return err
	}
	defer reg.Close()
	st, err := reg.Get(ctx, *siteName)
	if errors.Is(err, site.ErrNoSite) {
		st, err = reg.Create(ctx, *siteName, site.Settings{DefaultGet: *defaultGet})
	}
	if err != nil {
		return err
	}
	if _, err := site.Import(ctx, st, *out, site.ImportOptions{AuthoredFromLive: true}); err != nil {
		return err
	}
	fmt.Printf("imported into site %s\n", *siteName)
	return nil
}

func readKeyFile(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && strings.TrimSpace(k) == name {
			return strings.Trim(strings.TrimSpace(v), `"'`), nil
		}
	}
	return "", fmt.Errorf("%s not found in %s", name, path)
}
