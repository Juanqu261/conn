// Package webui serves Conn Lab, the single-page web application built from
// apps/web and embedded in conn server (decision 0013). A build without the
// web assets still compiles: the site directory then holds only .gitkeep,
// and every Lab route answers with a page explaining how to build it.
package webui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:site
var site embed.FS

//go:embed placeholder.html
var placeholder []byte

// ContentSecurityPolicy is sent with the Lab page. Scripts, styles, fonts,
// and connections come only from this origin; GitHub avatars are the one
// external image source, and sign-in posts only to this origin or GitHub.
const ContentSecurityPolicy = "default-src 'self'; img-src 'self' https://avatars.githubusercontent.com; " +
	"connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; " +
	"form-action 'self' https://github.com"

// placeholderPolicy allows only the placeholder's inline style.
const placeholderPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'"

// Assets returns the embedded Lab build, or nil when this binary was built
// without it.
func Assets() fs.FS {
	sub, err := fs.Sub(site, "site")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// Handler serves assets: hashed files under /assets/ with immutable
// caching, other existing files (such as the favicon) briefly cached, and
// index.html for every other path so the client router can resolve it.
// Nil assets serve the build placeholder. Only GET and HEAD are allowed.
func Handler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("X-Frame-Options", "DENY")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			header.Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if assets == nil {
			header.Set("Content-Security-Policy", placeholderPolicy)
			header.Set("Cache-Control", "no-store")
			header.Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(placeholder)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if strings.HasPrefix(name, "assets/") {
			if !serveFile(w, r, assets, name, "public, max-age=31536000, immutable") {
				http.NotFound(w, r)
			}
			return
		}
		if name != "" && name != "index.html" && path.Ext(name) != "" &&
			serveFile(w, r, assets, name, "public, max-age=3600") {
			return
		}
		header.Set("Content-Security-Policy", ContentSecurityPolicy)
		if !serveFile(w, r, assets, "index.html", "no-cache") {
			http.NotFound(w, r)
		}
	})
}

// serveFile writes one regular file from assets and reports whether it
// existed.
func serveFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name, cacheControl string) bool {
	file, err := assets.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, path.Base(name), info.ModTime(), seeker)
	return true
}
