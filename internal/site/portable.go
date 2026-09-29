package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/store"
)

// ExportFormat identifies the portable directory layout.
const ExportFormat = "pagelike-export/1"

// Manifest describes an exported site. Documents are written byte-exact
// under files/<path>. Secrets, identities, sessions, stream history and
// transient state are never exported.
type Manifest struct {
	Format      string         `json:"format"`
	Site        string         `json:"site"`
	ExportedAt  time.Time      `json:"exported_at"`
	State       string         `json:"state"` // "live" or "authored"
	Generation  int64          `json:"generation"`
	Version     string         `json:"version"` // authored digest at export
	Settings    PortableConfig `json:"settings"`
	Documents   []ManifestDoc  `json:"documents"`
	Dirs        []string       `json:"dirs,omitempty"`
	Authored    []ManifestDoc  `json:"authored,omitempty"`
	NotExported []string       `json:"not_exported"`
}

// ManifestDoc lists one exported document.
type ManifestDoc struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag,omitempty"`
	File        string `json:"file"`
}

// PortableConfig is the non-secret subset of Settings.
type PortableConfig struct {
	DefaultGet   string   `json:"default_get"`
	Cookies      string   `json:"cookies,omitempty"`
	LoginPath    string   `json:"login_path,omitempty"`
	LogoutPath   string   `json:"logout_path,omitempty"`
	CallbackPath string   `json:"callback_path,omitempty"`
	MaxBodyBytes int64    `json:"max_body_bytes,omitempty"`
	OIDCIssuer   string   `json:"oidc_issuer,omitempty"` // client id/secret are not exported
	Lineage      *Lineage `json:"lineage,omitempty"`
}

var notExported = []string{
	"end-user accounts, password hashes and OIDC client secrets (supply fresh identity configuration on import)",
	"sessions and transient (session-scoped) element state",
	"SSE event history (subscribers resynchronise with a fresh read)",
	"pending outbound HTTP deliveries",
	"authoring API keys",
	"participation records (they reference identities of the source site)",
}

// Export writes the site to dir. state "live" exports current documents
// plus the authored baseline; "authored" exports only the authored baseline.
func Export(ctx context.Context, s *Site, dir, state string) (*Manifest, error) {
	if state == "" {
		state = "live"
	}
	if state != "live" && state != "authored" {
		return nil, fmt.Errorf("export state must be live or authored")
	}
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return nil, err
	}
	gen, _ := s.Store.Generation(ctx)
	ver, _ := s.Store.AuthoredDigest(ctx)
	set := s.Settings()
	m := &Manifest{Format: ExportFormat, Site: s.Name, ExportedAt: time.Now().UTC(), State: state, Generation: gen, Version: ver,
		Settings: portable(set), NotExported: notExported}
	authored, err := s.Store.Authored(ctx)
	if err != nil {
		return nil, err
	}
	var docs []*store.Document
	if state == "live" {
		if docs, err = s.Store.List(ctx, "/", true); err != nil {
			return nil, err
		}
	} else {
		docs = authored
	}
	for _, d := range docs {
		md, err := writeDoc(s, dir, "files", d)
		if err != nil {
			return nil, err
		}
		m.Documents = append(m.Documents, md)
	}
	if state == "live" {
		for _, d := range authored {
			md, err := writeDoc(s, dir, "authored", d)
			if err != nil {
				return nil, err
			}
			m.Authored = append(m.Authored, md)
		}
	}
	m.Dirs, _ = s.Store.Dirs(ctx, "/")
	b, _ := json.MarshalIndent(m, "", "  ")
	return m, os.WriteFile(filepath.Join(dir, "pagelike-export.json"), b, 0o644)
}

func portable(set Settings) PortableConfig {
	pc := PortableConfig{DefaultGet: set.DefaultGet, Cookies: set.Cookies, LoginPath: set.LoginPath, LogoutPath: set.LogoutPath,
		CallbackPath: set.CallbackPath, MaxBodyBytes: set.MaxBodyBytes, Lineage: set.Lineage}
	if set.OIDC != nil {
		pc.OIDCIssuer = set.OIDC.Issuer
	}
	return pc
}

func writeDoc(s *Site, dir, sub string, d *store.Document) (ManifestDoc, error) {
	rel := filepath.Join(sub, filepath.FromSlash(strings.TrimPrefix(d.Path, "/")))
	if strings.HasSuffix(d.Path, "/") || strings.Contains(d.Path, "/../") {
		return ManifestDoc{}, fmt.Errorf("refusing to export odd path %q", d.Path)
	}
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ManifestDoc{}, err
	}
	var r io.Reader = bytes.NewReader(d.Body)
	if d.BlobSHA != "" {
		f, err := s.Store.OpenBlob(d.BlobSHA)
		if err != nil {
			return ManifestDoc{}, err
		}
		defer f.Close()
		r = f
	}
	out, err := os.Create(full)
	if err != nil {
		return ManifestDoc{}, err
	}
	n, err := io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return ManifestDoc{Path: d.Path, ContentType: d.ContentType, Size: n, ETag: d.ETag, File: filepath.ToSlash(rel)}, err
}

// ImportOptions control Import.
type ImportOptions struct {
	// AuthoredFromLive records every imported live document as authored
	// baseline when the export carries no separate authored set (e.g. an
	// export taken from PageLove over WebDAV).
	AuthoredFromLive bool
	Lineage          *Lineage
}

// Import loads an export directory into an empty site in one transaction.
func Import(ctx context.Context, s *Site, dir string, opt ImportOptions) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "pagelike-export.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Format != ExportFormat {
		return nil, fmt.Errorf("unsupported export format %q", m.Format)
	}
	existing, err := s.Store.List(ctx, "/", false)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, errors.New("import target site is not empty")
	}
	type loaded struct {
		doc  store.Document
		kind string
	}
	var docs []loaded
	load := func(md ManifestDoc, kind string) error {
		if !strings.HasPrefix(md.Path, "/") || strings.Contains(md.Path, "..") {
			return fmt.Errorf("bad path %q in manifest", md.Path)
		}
		full := filepath.Join(dir, filepath.FromSlash(md.File))
		if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(dir)+string(filepath.Separator)) {
			return fmt.Errorf("file %q escapes the export directory", md.File)
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		d := store.Document{Path: md.Path, ContentType: md.ContentType}
		if IsMarkup(md.ContentType) {
			d.Body = body
		} else {
			sha, size, err := s.Store.WriteBlob(bytes.NewReader(body))
			if err != nil {
				return err
			}
			d.BlobSHA, d.Size = sha, size
		}
		docs = append(docs, loaded{d, kind})
		return nil
	}
	for _, md := range m.Documents {
		if err := load(md, "live"); err != nil {
			return nil, err
		}
	}
	for _, md := range m.Authored {
		if err := load(md, "authored"); err != nil {
			return nil, err
		}
	}
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	_, err = s.Store.Update(ctx, func(tx *store.Tx) error {
		for _, l := range docs {
			d := l.doc
			switch l.kind {
			case "live":
				stored, err := tx.Put(&d)
				if err != nil {
					return err
				}
				if m.State == "authored" || (opt.AuthoredFromLive && len(m.Authored) == 0) {
					if err := tx.PutAuthored(stored); err != nil {
						return err
					}
				}
			case "authored":
				if IsMarkup(d.ContentType) {
					d.Size = int64(len(d.Body))
				}
				if err := tx.PutAuthored(&d); err != nil {
					return err
				}
			}
		}
		for _, p := range m.Dirs {
			if err := tx.MkDir(p); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = s.UpdateSettings(ctx, func(cur *Settings) {
		cur.DefaultGet = m.Settings.DefaultGet
		if m.Settings.Cookies != "" {
			cur.Cookies = m.Settings.Cookies
		}
		cur.LoginPath, cur.LogoutPath, cur.CallbackPath = m.Settings.LoginPath, m.Settings.LogoutPath, m.Settings.CallbackPath
		if m.Settings.MaxBodyBytes > 0 {
			cur.MaxBodyBytes = m.Settings.MaxBodyBytes
		}
		cur.Lineage = m.Settings.Lineage
		if opt.Lineage != nil {
			cur.Lineage = opt.Lineage
		}
	})
	return &m, err
}

// Fork creates dst as an independent remix of src: it copies src's authored
// baseline (code and authored content) and settings, but no participant
// contributions, identities, sessions, keys or stream history. dst records
// its lineage (source site and authored version).
func Fork(ctx context.Context, reg *Registry, src, dst, note string) (*Site, error) {
	from, err := reg.Get(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("source site %q: %w", src, err)
	}
	tmp, err := os.MkdirTemp("", "pagelike-fork-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	m, err := Export(ctx, from, tmp, "authored")
	if err != nil {
		return nil, err
	}
	gen, _ := from.Store.Generation(ctx)
	lin := &Lineage{ForkedFrom: src, ForkedAt: time.Now().UTC().Format(time.RFC3339), SourceGen: gen, SourceDigest: m.Version, Note: note}
	to, err := reg.Create(ctx, dst, Settings{DefaultGet: from.Settings().DefaultGet})
	if err != nil {
		return nil, err
	}
	if _, err := Import(ctx, to, tmp, ImportOptions{Lineage: lin}); err != nil {
		reg.Delete(dst)
		return nil, err
	}
	return to, nil
}
