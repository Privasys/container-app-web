// Copyright (c) Privasys. All rights reserved.
// Licensed under the GNU Affero General Public License v3.0. See LICENSE.

// Command container-app-web is a template for a container app on the
// Privasys platform that serves three surfaces from one image:
//
//   - private: everything not declared otherwise. Reached only over a sealed
//     session (a browser through the auth SDK) or RA-TLS (the CLI, services,
//     other enclaves). The runtime answers 403 sealed-transport-required to
//     anything else, before the app sees the request.
//   - static: the browser shell, served in the clear so the page can load
//     before a sealed session exists. GET and HEAD only, never under /api.
//   - public: explicit method and path pairs served in the clear: a public
//     JSON read and an inbound webhook. Anonymous by construction: the
//     runtime strips every credential and identity header on the way in.
//
// The declaration lives in privasys.json (the surfaces block) and rides the
// measured image label, so a verifier reads off the image exactly what this
// app serves in the clear. The handlers are grouped by surface so you can
// delete the ones you do not need.
package main

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

//go:embed static
var staticFiles embed.FS

const (
	// transportHeader is set by the runtime on every request and tells the
	// app which leg it arrived on: "public" (in the clear, on a declared
	// surface), "sealed" (a sealed session) or "attested" (RA-TLS direct).
	// An inbound value is stripped, so the app can trust it.
	transportHeader = "X-Privasys-Transport"
	// subjectHeader is the per-app subject the session relay asserts for a
	// signed-in browser session. Never present on a public request.
	subjectHeader = "X-Privasys-Sub"
	// signatureHeader carries the webhook sender's HMAC. It reaches the app
	// only because the manifest lists it on the route; any other custom
	// header is dropped by the runtime on a public route.
	signatureHeader = "X-Demo-Signature"
	// maxWebhookBody is how much of a webhook body the app reads.
	maxWebhookBody = 1 << 20
	// ringSize is how many webhook events the app keeps in memory.
	ringSize = 100
)

func main() {
	// The platform runs containers on the host network and injects a unique
	// PORT per app. 8080 is only the default for a local run.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	secret := os.Getenv("DEMO_WEBHOOK_SECRET")
	if secret == "" {
		secret = "demo"
	}
	store := newEventStore(ringSize, eventLogPath())
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(store, secret),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("container-app-web %s listening on :%s (%d events loaded)", version, port, store.Len())
	log.Fatal(srv.ListenAndServe())
}

// eventLogPath is the append-only event log on the app's sealed volume, or
// "" when the app runs without one (a local run without /data).
func eventLogPath() string {
	if st, err := os.Stat("/data"); err == nil && st.IsDir() {
		return "/data/events.log"
	}
	return ""
}

// newHandler wires the three surfaces plus the probes. The runtime decides
// which surface a request is on; the groups below only keep the code honest
// about what each route may assume.
func newHandler(store *eventStore, secret string) http.Handler {
	mux := http.NewServeMux()
	addStatic(mux)
	addPublic(mux, store, secret)
	addPrivate(mux, store)

	// Probes. The runtime's readiness check is exempt from the
	// sealed-transport rule, so these need no declaration.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "container-app-web", "version": version})
	})
	return mux
}

// Static surface: the browser shell. Measured code, no data, served in the
// clear so the page can load before a sealed session exists. The runtime
// serves it GET/HEAD only and sets Referrer-Policy: same-origin.
func addStatic(mux *http.ServeMux) {
	shell, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, shell, "index.html")
	})
	assets := http.FileServerFS(shell)
	mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r) // no directory listings
			return
		}
		assets.ServeHTTP(w, r)
	})
}

// Public surface: explicit routes served in the clear. Nothing on these
// requests identifies a caller (the runtime removed Authorization, Cookie
// and every X-Privasys-* header except the transport marker), and nothing in
// these responses may identify one either. Write them as if they were
// printed in a newspaper.
func addPublic(mux *http.ServeMux, store *eventStore, secret string) {
	// GET is declared public. The same path also answers POST, which is how
	// the platform invokes the "status" tool over RA-TLS: a POST here is not
	// declared, so it is private.
	status := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=10")
		writeJSON(w, http.StatusOK, map[string]any{
			"app":       "container-app-web",
			"version":   version,
			"transport": r.Header.Get(transportHeader),
			"events":    store.Len(),
			"time":      time.Now().UTC().Format(time.RFC3339),
		})
	}
	mux.HandleFunc("GET /api/v1/public/status", status)
	mux.HandleFunc("POST /api/v1/public/status", status)

	// An inbound webhook. The sender cannot run our transport, so the
	// payload carries its own proof: an HMAC over the body under a secret
	// shared with the sender. The gateway reads this body in transit, so
	// keep webhook payloads minimal and fetch the details back over the
	// private surface when they matter.
	mux.HandleFunc("POST /api/v1/webhooks/demo", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable body"})
			return
		}
		if !verifySignature(secret, body, r.Header.Get(signatureHeader)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad signature"})
			return
		}
		ev := store.Add(r.Header.Get("Content-Type"), body)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "id": ev.ID})
	})
}

// Private surface: everything else. Only a sealed session or an RA-TLS
// connection reaches these handlers, so the identity headers are real.
func addPrivate(mux *http.ServeMux, store *eventStore) {
	mux.Handle("GET /api/v1/me", private(http.HandlerFunc(whoAmI)))
	events := private(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"events": store.List()})
	}))
	mux.Handle("GET /api/v1/events", events)
	mux.Handle("POST /api/v1/events", events) // the "events" tool
}

// private refuses a request that arrived in the clear. The runtime never
// routes a public request to an undeclared path, so this is defence in
// depth: the one line that stops a handler copied onto a public route from
// serving anything.
func private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(transportHeader) == "public" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "private route"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// whoAmI reports the identity the request carries. Over a sealed session
// from a voucher bootstrap the relay asserts the per-app subject. After a
// fresh sign-in ceremony the relay does not learn the subject: the SDK sends
// the identity provider's bearer as the inner Authorization instead, and
// the app verifies it.
func whoAmI(w http.ResponseWriter, r *http.Request) {
	subject := r.Header.Get(subjectHeader)
	note := "no identity on this request (a local run, or an anonymous sealed session)"
	switch {
	case subject != "":
		note = "subject asserted by the session relay: the per-app subject of the signed-in user"
	case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
		// This template does not verify the token. A real app checks its
		// signature against the identity provider's JWKS, its issuer,
		// audience and expiry, and reads the subject from it:
		// https://docs.privasys.org/solutions/privasys-id/idp
		note = "a bearer token was presented; this template does not verify it"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"subject":   subject,
		"transport": r.Header.Get(transportHeader),
		"note":      note,
	})
}
