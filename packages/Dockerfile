# Build context is packages/, not the repo root: the api imports the collector
# package, so both must be inside the context for the module to resolve.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Copy manifests first so dependency download is cached separately from source
# changes - otherwise every edit re-downloads the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off produces a static binary, which is what lets the runtime stage be
# a bare alpine with nothing else in it.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./api/cmd/api

FROM alpine:3.21
# ca-certificates is required: the Steam OpenID verification call is HTTPS, and
# without root certs it fails with an opaque x509 error.
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
USER app
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
