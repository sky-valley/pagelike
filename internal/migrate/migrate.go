// Package migrate copies a site out of a WebDAV authoring mount — PageLove's
// per-host WebDAV URL, or another pagelike instance — into a pagelike export
// directory (see internal/site ExportFormat) that `pagelike import` loads.
//
// What is copied: every stored file, byte for byte, with the content type the
// mount reports. HTML is fetched over the authoring plane, so it is the stored
// source, not a composed page. What is not copied (and cannot be, over WebDAV):
// identities and sessions, OIDC client secrets, API keys, transient
// (session-scoped) state, stream history, pending outbound deliveries, and
// host settings other than those passed explicitly (e.g. default-GET mode).
package migrate

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/site"
)

// Source describes a WebDAV mount.
type Source struct {
	URL        string // e.g. https://dav-host.onpagelove.com/
	BearerKey  string // authoring key (never written anywhere)
	DefaultGet string // host default-GET mode to record ("allow"/"deny"; "" = allow)
	Client     *http.Client
	// Pace bounds the request rate (0 = 4 requests per second).
	Pace time.Duration
	// Exclude lists path prefixes that are not copied (e.g. test scratch).
	Exclude []string
	Log     func(format string, args ...any)
}

// Entry is one file found on the mount.
type Entry struct {
	Path        string
	ContentType string
	Size        int64
	ETag        string
}

type multistatus struct {
	Responses []struct {
		Href     string `xml:"href"`
		Propstat []struct {
			Prop struct {
				ResourceType struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"resourcetype"`
				ContentType   string `xml:"getcontenttype"`
				ContentLength int64  `xml:"getcontentlength"`
				ETag          string `xml:"getetag"`
			} `xml:"prop"`
			Status string `xml:"status"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (s *Source) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (s *Source) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

func (s *Source) pace() {
	d := s.Pace
	if d == 0 {
		d = 250 * time.Millisecond
	}
	time.Sleep(d)
}

func (s *Source) do(ctx context.Context, method, p string, hdr map[string]string) (*http.Response, error) {
	base, err := url.Parse(strings.TrimSuffix(s.URL, "/"))
	if err != nil {
		return nil, err
	}
	u := *base
	u.Path = base.Path + p
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.BearerKey)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	s.pace()
	return s.client().Do(req)
}

// List walks the mount from dir (ending in '/') and returns every file and
// every (possibly empty) collection path.
func (s *Source) List(ctx context.Context, dir string) (files []Entry, dirs []string, err error) {
	seen := map[string]bool{}
	queue := []string{dir}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] {
			continue
		}
		seen[d] = true
		resp, err := s.do(ctx, "PROPFIND", d, map[string]string{"Depth": "1", "Content-Type": "application/xml"})
		if err != nil {
			return nil, nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMultiStatus {
			return nil, nil, fmt.Errorf("PROPFIND %s: %d %s", d, resp.StatusCode, truncate(string(body), 200))
		}
		var ms multistatus
		if err := xml.Unmarshal(body, &ms); err != nil {
			return nil, nil, fmt.Errorf("PROPFIND %s: %w", d, err)
		}
		if d != "/" {
			dirs = append(dirs, d)
		}
		for _, r := range ms.Responses {
			href, err := url.PathUnescape(r.Href)
			if err != nil {
				href = r.Href
			}
			if u, err := url.Parse(href); err == nil && u.Path != "" {
				href = u.Path
			}
			if len(r.Propstat) == 0 {
				continue
			}
			pr := r.Propstat[0].Prop
			if pr.ResourceType.Collection != nil {
				if !strings.HasSuffix(href, "/") {
					href += "/"
				}
				if href != d {
					queue = append(queue, href)
				}
				continue
			}
			files = append(files, Entry{Path: href, ContentType: pr.ContentType, Size: pr.ContentLength, ETag: pr.ETag})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Strings(dirs)
	return files, dirs, nil
}

// Export downloads every file under the mount into an export directory.
func (s *Source) Export(ctx context.Context, out, siteName string) (*site.Manifest, error) {
	files, dirs, err := s.List(ctx, "/")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(out, "files"), 0o755); err != nil {
		return nil, err
	}
	dg := s.DefaultGet
	if dg == "" {
		dg = "allow"
	}
	m := &site.Manifest{Format: site.ExportFormat, Site: siteName, ExportedAt: time.Now().UTC(), State: "live",
		Settings: site.PortableConfig{DefaultGet: dg}, Dirs: dirs,
		NotExported: []string{
			"identities, sessions and OIDC client secrets (configure the pagelike site's identity and link old subjects with `pagelike user link`)",
			"API keys (mint new authoring keys with `pagelike key create`)",
			"transient (session-scoped) element state",
			"SSE event history (clients resynchronise with a fresh read)",
			"pending outbound HTTP deliveries",
			"host settings other than default-GET mode (plan, aliases, OIDC)",
		}}
	keep := func(p string) bool {
		for _, x := range s.Exclude {
			if x != "" && strings.HasPrefix(p, x) {
				return false
			}
		}
		return true
	}
	var kept []string
	for _, d := range dirs {
		if keep(d) {
			kept = append(kept, d)
		}
	}
	m.Dirs = kept
	for _, f := range files {
		if !keep(f.Path) {
			continue
		}
		if strings.Contains(f.Path, "/../") || !strings.HasPrefix(f.Path, "/") {
			return nil, fmt.Errorf("refusing odd path %q", f.Path)
		}
		resp, err := s.do(ctx, http.MethodGet, (&url.URL{Path: f.Path}).EscapedPath(), nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %d", f.Path, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = f.ContentType
		}
		if mt, params, err := mime.ParseMediaType(ct); err == nil && (mt == "text/html") {
			_ = params
			ct = "text/html"
		}
		rel := filepath.Join("files", filepath.FromSlash(strings.TrimPrefix(f.Path, "/")))
		full := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			return nil, err
		}
		m.Documents = append(m.Documents, site.ManifestDoc{Path: f.Path, ContentType: ct, Size: int64(len(body)), ETag: resp.Header.Get("ETag"), File: filepath.ToSlash(rel)})
		s.logf("copied %s (%d bytes, %s)", f.Path, len(body), ct)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "pagelike-export.json"), b, 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// SafeJoin is exported for callers that write export files.
func SafeJoin(root, p string) string { return filepath.Join(root, path.Clean("/"+p)) }
