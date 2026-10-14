# container-app-web: one small binary, three surfaces.
#
# Nothing is fetched at runtime, so the image the platform measures is the
# whole of what runs. The build is single-arch and provenance-free on
# purpose: an OCI attestation index would change the manifest digest the
# enclave pins at OID 1.3.6.1.4.1.65230.3.2.
#
# The surfaces declaration (privasys.json) is not copied here: CI embeds it
# as the org.privasys.manifest label, which is inside the digest-pinned
# image config. That label is the declaration; the older
# org.privasys.static-unsealed-prefixes label is only a deprecated alias
# for its static list.

FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY static ./static

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/container-app-web .

# The app makes no outbound calls, so it needs no trust store: a static
# base with nothing but the binary.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /out/container-app-web /container-app-web

LABEL org.opencontainers.image.source="https://github.com/Privasys/container-app-web"
LABEL org.opencontainers.image.title="container-app-web"
LABEL org.opencontainers.image.description="Privasys template: a container app with private, static and public surfaces"

# The platform runs containers on the host network and injects a unique
# PORT per app, which overrides this default. 8080 and EXPOSE are for a
# local docker run.
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/container-app-web"]
