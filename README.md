# container-app-web

A template container app for the Privasys confidential platform
(`enclave-os-virtual`, TDX) that serves a web page and a public API next
to its private data. One small Go binary, standard library only, written
so you can delete what you do not need.

It exists to show the **three surfaces** an app has and what each one
costs. Everything an app serves is private unless the app declares
otherwise in its measured manifest, and what it declares public is served
in the clear, visible to verifiers, and anonymous by construction.

| Route | Surface | What it does |
| --- | --- | --- |
| `GET /`, `GET /assets/*` | static | The browser shell: one page, one script, embedded in the binary. |
| `GET /api/v1/public/status` | public | JSON status, cacheable, no identity. |
| `POST /api/v1/webhooks/demo` | public | Accepts a signed event, stores it. |
| `GET /api/v1/me` | private | Who the caller is. |
| `GET /api/v1/events` | private | The stored events. |
| `GET /health`, `GET /version` | probes | Readiness and the build version. |

## 1. The three surfaces

| Surface | Reached over | Methods | Who sees the traffic | Identity you may trust | What it costs | When to use it |
| --- | --- | --- | --- | --- | --- | --- |
| **private** (the default: everything not declared) | RA-TLS, or a sealed session through the gateway | any | the client and the enclave only | `X-Privasys-Sub` (the per-app subject), the inner bearer, the mutual RA-TLS caller, the payer headers | a session slot or an attested connection per client; can carry a price | anything about a person, anything a caller pays for |
| **static** (declared prefixes) | in the clear through the gateway | GET, HEAD | the gateway | none | nothing beyond the owner's compute; rate limited per client | the browser shell: code and text that are in the image anyway |
| **public** (declared method and path pairs) | in the clear through the gateway | GET, HEAD, POST, PUT, PATCH, DELETE as declared; OPTIONS answered by the runtime | the gateway | none | the owner's compute; cannot carry a price; rate limited per client | data you would print in a newspaper, and inbound events from senders that cannot run our transport |

What "in the clear" means: the gateway, a Privasys-operated VM that is not
part of the attested enclave, terminates TLS on these routes with a
publicly trusted certificate and can read the path, the query, the headers
and the body in both directions. It logs TLS metadata and route outcomes
only, never a header, a query string or a body, on any leg, and it keeps
nothing. A caller on a clear route still knows the gateway opened RA-TLS to
the enclave and refused to forward to one that did not match the route's
policy. That is the same tier of trust as a non-wallet sign-in: trusting
Privasys operations, not hardware.

Private routes are reachable only over a sealed session (a browser, through
the [auth SDK](https://docs.privasys.org/solutions/privasys-id/auth-sdk))
or RA-TLS (the CLI, services, other enclaves). Anything else gets
`403 sealed-transport-required` from the runtime before your code runs.

On a public or static request the runtime enforces, rather than advises:

- **Anonymous by construction.** Request headers are reduced to an
  allow-list (`Accept`, `Accept-Encoding`, `Accept-Language`,
  `Cache-Control`, `Content-Type`, `Content-Length`, `If-None-Match`,
  `If-Modified-Since`, `Range`, `User-Agent`, `Origin`, the client address
  in `X-Forwarded-For`) plus the headers a route declares. `Authorization`,
  `Cookie` and every `X-Privasys-*` header are deleted, and a credential
  that arrived anyway is counted and shown to you in the portal, so you
  learn on dev that a page sent one. No identity is ever asserted.
- **Response headers are reduced too**: `Content-Type`, `Content-Length`,
  `Content-Encoding`, `Cache-Control`, `ETag`, `Last-Modified`,
  `Location`, `Retry-After`, `Vary` and a few more. `Set-Cookie` never
  leaves, so there is never a cookie on your origin for a browser to send
  back. An `X-User-Id` you set by mistake is dropped.
- **The app can tell.** `X-Privasys-Transport` is `public` on the clear
  leg, `sealed` from a sealed session, `attested` over RA-TLS. The runtime
  strips any inbound value, so the app can trust it.
- **No price.** A public route cannot carry a price; the runtime refuses
  the image at load if the manifest tries.
- **Bounded.** A per-client token bucket, 10 requests per second with a
  burst of 50 by default; a full bucket answers 429 with `Retry-After`.
- **Preflight.** `OPTIONS` on a public path is answered by the runtime
  (204, `Access-Control-Allow-Origin: *`, the declared methods and
  headers, no credentials) when `cors` is `"*"`.
- **No upgrade.** A WebSocket on a public path is 403. The only browser
  socket is the sealed WebSocket mux, on the private surface.
- **Logged.** One structured line per public request (method, path
  without its query, status, client, duration) in the enclave log.

## 2. Decision guide

**An attested client** (the wallet, the CLI, a service on the verification
libraries, another enclave). Private, over RA-TLS. The client verifies the
enclave itself; identity is the bearer, the mutual RA-TLS identity or a
spend token.

**A browser user who signed in.** Private, over a sealed session the auth
SDK opens for you. The wallet verified the enclave and the SDK inherits
that; identity is `X-Privasys-Sub` or the inner bearer.

**A browser visitor who has not signed in.** Static for the page, public
for the reads, both in the clear and gateway-vouched only. No identity, so
nothing about a person.

**A third-party machine** (a webhook sender, an OAuth redirect, a
federation peer, plain curl). Public, in the clear. No identity; the
payload carries its own proof, such as a signature or an OAuth state.

## 3. The manifest

[`privasys.json`](./privasys.json) rides the image as the
`org.privasys.manifest` label (CI base64-encodes it). The `surfaces` block
is the whole declaration:

```jsonc
{
  "tools": [ /* MCP tools, as in container-app-example */ ],
  "surfaces": {
    // Static prefixes, GET and HEAD only. "/" matches the root document
    // only; any other entry is a prefix. "/api" and "/api/..." are refused
    // at parse: the data plane is never static, declare a public route.
    "static": ["/", "/assets/"],

    // Public routes: explicit method and path pairs. A path ending in "/"
    // is a prefix, otherwise exact. A prefix never implies another method:
    // GET /api/v1/public/ does not make POST /api/v1/public/x public.
    "public": [
      { "method": "GET",  "path": "/api/v1/public/" },
      // "headers" lists the extra request headers this route needs beyond
      // the allow-list, typically the sender's signature header. A
      // credential header (Authorization, Cookie, X-Privasys-*) can never
      // be declared.
      { "method": "POST", "path": "/api/v1/webhooks/demo", "headers": ["X-Demo-Signature"] }
    ],

    // "*": public routes answer any origin, without credentials, and the
    // runtime answers OPTIONS. "same-origin" (the default): no
    // cross-origin headers at all, which is all your own page needs.
    "cors": "*",

    // Optional. The per-client rate limit on public and static routes.
    // Defaults: 10 requests per second, a burst of 50.
    "limits": { "rps": 10, "burst": 50 }
  },
  "store": { /* the store listing: tagline, description, category */ }
}
```

The older image label `org.privasys.static-unsealed-prefixes` (a
comma-separated list of static prefixes) still works as a deprecated alias
for `surfaces.static`; the runtime takes the union of both. New apps use
the manifest and do not set the label.

The two MCP tools, `status` and `events`, point at the two GET routes. The
platform invokes a tool with a POST over RA-TLS, and a POST on those paths
is not declared, so a tool call is always private, even for `status`. The
same handler answers both.

## 4. What changes the attestation

The manifest label is inside the digest-pinned image config, so the
surfaces are **measured**. Editing `surfaces` changes the image digest the
enclave pins at OID `1.3.6.1.4.1.65230.3.2`, which means:

- a verifier reads off the image exactly which routes the app serves in
  the clear, and the portal shows them on the app's Attestation tab and in
  the store listing;
- the owners re-approve the new measurement, as for any upgrade;
- when a version adds or widens a public or static surface, the platform
  asks an owner to confirm, once per version, that the new clear routes
  carry no personal data. `privasys apps deploy` and
  `privasys apps versions stage` print the diff of surfaces and prompt for
  it; the confirmation (who, when, the exact route list) is stored with
  the version.

## 5. What you must never do

- **Serve account data on a public route.** The gateway reads it. The
  runtime removes identity from the request precisely so that a public
  handler has nothing to look up; do not fetch it from somewhere else.
- **Trust a header on a public route.** Anyone can send any header, and
  the runtime has already deleted the ones that could mean anything. The
  one exception is a signature header you declared, which you verify
  yourself, as the webhook here does.
- **Price a public route.** There is no payer. The runtime refuses the
  image.
- **Point a vanity domain at anything but a 301 to the enclave host.** A
  proxy in front of the gateway would be a second party reading your
  traffic, on every surface.
- **Put secrets in URLs.** Paths and query strings are visible to the
  gateway on every terminated-leg request, sealed ones included. Keep
  page state in the fragment (`#...`), which never leaves the browser.

Webhook payloads deserve one more line: they transit the gateway in the
clear, because the sender chose a non-attested endpoint. Ask senders for
minimal, signed payloads and fetch the details back over the private
surface when they matter.

You are responsible for data you place on declared public and static
surfaces; Privasys forwards it and keeps none. See the
[developer terms](https://privasys.org/legal/terms/).

## 6. Build, deploy on dev, verify from outside

### Run locally

```bash
go test ./...
go run .                      # http://localhost:8080
```

Without the runtime in front nothing is enforced: the private routes
answer 200 with an empty subject, and `transport` is empty. The page says
so.

```bash
docker build -t container-app-web .
docker run --rm -e PORT=8080 -p 8080:8080 -v caw-data:/data container-app-web
```

### Build

A `v*` tag builds `ghcr.io/privasys/container-app-web:<tag>` through
[`.github/workflows/build.yml`](./.github/workflows/build.yml), with
`privasys.json` embedded as the manifest label, and cuts a release that
records the image digest. Every push and pull request runs the tests and
drives the image the way the platform and a sender would.

### Deploy on dev

```bash
privasys apps create --name my-web --source package --type container \
  --image ghcr.io/privasys/container-app-web:v0.1.0 --storage
privasys apps deploy my-web --version <version-id> --watch
privasys attest my-web
```

The deploy prompts you to confirm that the declared clear routes carry no
personal data (section 4). The app is then at
`https://<app>.apps.test.privasys.org`, where `<app>` is the hostname
`privasys apps describe my-web` prints. The webhook secret defaults to
`demo`; set `DEMO_WEBHOOK_SECRET` in the deployment to change it.

### Verify from outside

The four checks below are what the surfaces promise. Set `APP` to your
app's hostname.

```bash
APP=https://<app>.apps.test.privasys.org

# 1. The public read answers in the clear, and says so.
curl -si "$APP/api/v1/public/status"
#   HTTP/2 200
#   x-privasys-transport: public
#   access-control-allow-origin: *
#   cache-control: public, max-age=10
#   {"app":"container-app-web","version":"v0.1.0","transport":"public","events":0,"time":"..."}

# 2. The private route refuses a plain request before the app sees it.
curl -si "$APP/api/v1/me"
#   HTTP/2 403
#   sealed-transport-required

# 3. The public surface is bounded per client: a 429 appears after the burst.
for i in $(seq 1 60); do
  curl -s -o /dev/null -w '%{http_code}\n' "$APP/api/v1/public/status"
done | sort | uniq -c
#   200 for the first 50 or so, then 429 once the burst is spent (the
#   bucket refills at 10 per second, so the exact split varies)

# 4. Preflight is answered by the runtime, without credentials.
curl -si -X OPTIONS "$APP/api/v1/webhooks/demo" \
  -H 'Origin: https://example.com' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: X-Demo-Signature'
#   HTTP/2 204
#   access-control-allow-origin: *
#   access-control-allow-methods: POST
#   access-control-allow-headers: Accept, Content-Type, X-Demo-Signature
```

Then deliver a signed webhook, as a sender would, and read it back over
the private surface with the CLI (RA-TLS, after verifying the enclave):

```bash
SECRET=demo
BODY='{"event":"invoice.paid","id":"inv_123"}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.*= //')
curl -si -X POST "$APP/api/v1/webhooks/demo" \
  -H 'Content-Type: application/json' \
  -H "X-Demo-Signature: $SIG" \
  -d "$BODY"
#   HTTP/2 202
#   {"accepted":true,"id":0}

privasys apps call my-web events
#   {"events":[{"id":0,"received_at":"...","content_type":"application/json","body":"{\"event\":\"invoice.paid\",\"id\":\"inv_123\"}"}]}
```

Open `https://<app>.apps.test.privasys.org/` in a browser for the same
three calls from the page. The private buttons show the 403 on purpose;
a signed-in page would make those calls through the auth SDK's sealed
session instead, and `/api/v1/me` would answer with the user's per-app
subject.

## Notes for your own app

- Keep public handlers in their own group, as [`main.go`](./main.go)
  does, with nothing that can reach a session or a user record. The
  `private` wrapper is the one-line check every private handler keeps.
- The page's `publicFetch` in [`static/assets/app.js`](./static/assets/app.js)
  sends `credentials: 'omit'` and only `Accept`. Keep public calls that
  bare: a credential the browser attached has crossed the gateway before
  the runtime strips it.
- `/api/v1/events` is a unary read. Live push of new events to a signed-in
  page would use the sealed WebSocket mux on the private surface; a public
  route can never be upgraded.
- The bearer path in `/api/v1/me` is where a real app verifies the
  identity provider's token against its JWKS
  ([Privasys ID](https://docs.privasys.org/solutions/privasys-id/idp)).
- The events live in memory (the last 100) and in `/data/events.log` on
  the app's sealed volume when it has one, so they survive a restart.

## Related

- [container-app-example](https://github.com/Privasys/container-app-example):
  the minimal container app, no surfaces, boots ready.
- [container-app-example-with-config](https://github.com/Privasys/container-app-example-with-config):
  the configure-then-freeze pattern for apps that need a secret before
  they serve.
- [Deploy a container](https://docs.privasys.org/solutions/platform/developer-platform/deploy-container)
  in the documentation.

## Licence

[GNU Affero General Public License v3.0](LICENSE). See
[CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).
