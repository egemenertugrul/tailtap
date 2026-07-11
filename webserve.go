package main

import (
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// serveWeb runs a small file browser over HTTP on the given tailnet listener.
// It lists directories, serves files for download, and accepts uploads. Like
// the SSH server it is bound only to the tailnet, so the tailnet ACL is the
// only gate. Files are read and written as whoever started the binary. Enabled
// with -web; off by default.
func serveWeb(ln net.Listener, root string) error {
	return http.Serve(ln, &fileBrowser{root: root})
}

type fileBrowser struct{ root string }

func (h *fileBrowser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Contain every request within root: Clean with a leading slash drops any
	// ".." so the path can't escape the served directory.
	clean := path.Clean("/" + r.URL.Path)
	full := filepath.Join(h.root, filepath.FromSlash(clean))

	if r.Method == http.MethodPost {
		h.upload(w, r, full, clean)
		return
	}

	info, err := os.Stat(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		h.list(w, full, clean)
		return
	}
	http.ServeFile(w, r, full)
}

// list renders a directory: parent link, entries (folders first), and an
// upload form that posts back to the same directory.
func (h *fileBrowser) list(w http.ResponseWriter, dir, urlPath string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	if !strings.HasSuffix(urlPath, "/") {
		urlPath += "/"
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset=utf-8>`)
	b.WriteString(`<meta name=viewport content="width=device-width,initial-scale=1">`)
	fmt.Fprintf(&b, "<title>%s</title>", html.EscapeString(urlPath))
	b.WriteString(`<style>body{font:15px/1.6 system-ui,sans-serif;max-width:760px;margin:2rem auto;padding:0 1rem}` +
		`a{text-decoration:none}li{margin:.15rem 0}` +
		`form{margin-top:1.5rem;padding-top:1rem;border-top:1px solid #8884}</style>`)
	fmt.Fprintf(&b, "<h2>%s</h2><ul>", html.EscapeString(urlPath))
	if urlPath != "/" {
		b.WriteString(`<li><a href="../">../</a></li>`)
	}
	for _, e := range entries {
		disp := e.Name()
		if e.IsDir() {
			disp += "/"
		}
		href := (&url.URL{Path: disp}).String()
		fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, href, html.EscapeString(disp))
	}
	b.WriteString(`</ul>`)
	b.WriteString(`<form method=post enctype="multipart/form-data">` +
		`<input type=file name=file multiple> <button type=submit>Upload here</button></form>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

func (h *fileBrowser) upload(w http.ResponseWriter, r *http.Request, dir, urlPath string) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		http.Error(w, "upload target is not a directory", http.StatusBadRequest)
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, fh := range r.MultipartForm.File["file"] {
		if err := saveUpload(fh, dir); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, urlPath, http.StatusSeeOther)
}

func saveUpload(fh *multipart.FileHeader, dir string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	// filepath.Base drops any path in the client-supplied name, so an upload
	// can only land in the current directory.
	dst, err := os.Create(filepath.Join(dir, filepath.Base(fh.Filename)))
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}
