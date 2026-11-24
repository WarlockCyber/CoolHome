package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

// staticAssets serves the files embedded in the binary.
//
// The default http.FileServer is not enough here. Embedded files carry no
// modification time, so it sends neither Last-Modified nor Cache-Control, and a
// browser is then free to keep whatever copy it has. Since a new binary can
// serve different bytes under the same URL — the stylesheet and scripts are
// rebuilt into the executable — a tablet that had opened the dashboard once kept
// showing the previous interface after a restart, and the fix looked like it had
// not been applied.
//
// Each file gets an ETag derived from its content and `Cache-Control: no-cache`:
// the browser may keep the copy, but must ask before using it, and the answer is
// a cheap 304 unless the binary really changed.
type staticAssets struct {
	files map[string]staticFile
}

type staticFile struct {
	data  []byte
	etag  string
	ctype string
}

// contentTypes is deliberate rather than derived from the platform's mime
// table: a system lacking an entry for .js would serve it as text/plain, and
// with `X-Content-Type-Options: nosniff` the browser would then refuse to run
// the script at all.
var contentTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".ico":  "image/x-icon",
	".txt":  "text/plain; charset=utf-8",
}

// newStaticAssets reads every file into memory once. The assets are already
// embedded, so this allocates nothing that was not already in the binary.
func newStaticAssets(fsys fs.FS) (*staticAssets, error) {
	files := map[string]staticFile{}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		ctype := contentTypes[strings.ToLower(path.Ext(name))]
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		files[name] = staticFile{
			data:  data,
			etag:  `"` + hex.EncodeToString(sum[:8]) + `"`,
			ctype: ctype,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &staticAssets{files: files}, nil
}

// handler serves one path inside the embedded tree. It takes the name from the
// route rather than from the request path, so a request cannot walk out of the
// tree with "..".
func (s *staticAssets) handler(name string) http.Handler {
	file, ok := s.files[name]
	if !ok {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", file.ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", file.etag)
		// ServeContent answers If-None-Match with 304 and otherwise writes the
		// body; the zero time keeps a Last-Modified the binary cannot honour
		// from being sent.
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(file.data))
	})
}

// buildFingerprint identifies the bundle a binary carries: every embedded file
// by name and by hash. A page compares it with the one it was rendered with, so
// an open dashboard notices a restart and reloads itself instead of running the
// previous interface until somebody walks over and refreshes it by hand.
func buildFingerprint(fsys fs.FS) (string, error) {
	var names []string
	sizes := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		names = append(names, name)
		sizes[name] = hex.EncodeToString(sum[:8])
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(sizes[name]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// names lists what is served, for the router to register and for tests to
// assert on.
func (s *staticAssets) names() []string {
	out := make([]string, 0, len(s.files))
	for name := range s.files {
		out = append(out, name)
	}
	return out
}
