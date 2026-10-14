// Copyright (c) Privasys. All rights reserved.
// Licensed under the GNU Affero General Public License v3.0. See LICENSE.

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T) (http.Handler, *eventStore) {
	t.Helper()
	store := newEventStore(ringSize, "")
	return newHandler(store, "demo"), store
}

func do(t *testing.T, h http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	good := sign("demo", body)
	cases := []struct {
		name      string
		secret    string
		signature string
		want      bool
	}{
		{"valid", "demo", good, true},
		{"valid upper case", "demo", strings.ToUpper(good), true},
		{"valid with whitespace", "demo", " " + good + "\n", true},
		{"wrong secret", "other", good, false},
		{"tampered", "demo", "00" + good[2:], false},
		{"missing", "demo", "", false},
		{"not hex", "demo", "zz" + good[2:], false},
		{"truncated", "demo", good[:20], false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := verifySignature(c.secret, body, c.signature); got != c.want {
				t.Fatalf("verifySignature = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPublicStatus(t *testing.T) {
	h, store := newTestHandler(t)
	store.Add("application/json", []byte(`{"n":1}`))

	rec := do(t, h, http.MethodGet, "/api/v1/public/status", nil, map[string]string{transportHeader: "public"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=10" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	var body struct {
		App       string `json:"app"`
		Version   string `json:"version"`
		Transport string `json:"transport"`
		Events    int    `json:"events"`
		Time      string `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body.String())
	}
	if body.App != "container-app-web" || body.Version != version || body.Transport != "public" || body.Events != 1 || body.Time == "" {
		t.Fatalf("unexpected body: %+v", body)
	}

	// The tool call: a POST on the same path, which the manifest does not
	// declare public, so it arrives attested.
	rec = do(t, h, http.MethodPost, "/api/v1/public/status", []byte(`{}`), map[string]string{transportHeader: "attested"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", rec.Code)
	}
}

func TestWebhook(t *testing.T) {
	h, store := newTestHandler(t)
	body := []byte(`{"event":"ping"}`)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/demo", body, map[string]string{
		transportHeader: "public", signatureHeader: sign("demo", body), "Content-Type": "application/json",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed delivery = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if store.Len() != 1 || store.List()[0].Body != string(body) {
		t.Fatalf("event not stored: %+v", store.List())
	}

	rec = do(t, h, http.MethodPost, "/api/v1/webhooks/demo", body, map[string]string{
		transportHeader: "public", signatureHeader: sign("wrong", body),
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d, want 401", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/api/v1/webhooks/demo", body, map[string]string{transportHeader: "public"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no signature = %d, want 401", rec.Code)
	}
	if store.Len() != 1 {
		t.Fatalf("a refused delivery was stored")
	}

	big := bytes.Repeat([]byte("x"), maxWebhookBody+1)
	rec = do(t, h, http.MethodPost, "/api/v1/webhooks/demo", big, map[string]string{
		transportHeader: "public", signatureHeader: sign("demo", big),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body = %d, want 413", rec.Code)
	}
}

func TestRingKeepsTheLastEvents(t *testing.T) {
	store := newEventStore(3, "")
	for i := 0; i < 5; i++ {
		store.Add("", []byte{byte('a' + i)})
	}
	got := store.List()
	if len(got) != 3 || got[0].Body != "c" || got[2].Body != "e" || got[2].ID != 4 {
		t.Fatalf("ring = %+v", got)
	}
}

func TestPrivateRefusesPublicTransport(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, path := range []string{"/api/v1/me", "/api/v1/events"} {
		rec := do(t, h, http.MethodGet, path, nil, map[string]string{transportHeader: "public", subjectHeader: "should-never-be-here"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s on the clear leg = %d, want 403", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "should-never-be-here") {
			t.Fatalf("%s echoed an identity on a public request", path)
		}
	}
}

func TestWhoAmI(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := do(t, h, http.MethodGet, "/api/v1/me", nil, map[string]string{transportHeader: "sealed", subjectHeader: "sub-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("sealed = %d, want 200", rec.Code)
	}
	var me struct {
		Subject   string `json:"subject"`
		Transport string `json:"transport"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Subject != "sub-123" || me.Transport != "sealed" {
		t.Fatalf("unexpected body: %+v", me)
	}

	rec = do(t, h, http.MethodGet, "/api/v1/me", nil, map[string]string{transportHeader: "attested", "Authorization": "Bearer abc"})
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Subject != "" || !strings.Contains(me.Note, "bearer") || strings.Contains(rec.Body.String(), "abc") {
		t.Fatalf("unexpected body for a bearer: %s", rec.Body.String())
	}
}

func TestStaticShell(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := do(t, h, http.MethodGet, "/", nil, map[string]string{transportHeader: "public"})
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "container-app-web") || !strings.Contains(rec.Body.String(), "/assets/app.js") {
		t.Fatalf("the shell does not look like ours: %s", rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/assets/app.js", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("GET /assets/app.js = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "credentials: 'omit'") {
		t.Fatalf("publicFetch must omit credentials")
	}

	for _, path := range []string{"/assets/", "/assets/missing.js", "/index.html", "/nothing"} {
		if rec = do(t, h, http.MethodGet, path, nil, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, rec.Code)
		}
	}

	rec = do(t, h, http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d", rec.Code)
	}
}
