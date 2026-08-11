# Build the frontend and the manual, then the binary that embeds both, then
# ship only the binary. The result is a scratch-based image containing one
# file.

FROM node:26-alpine AS web
WORKDIR /src
# Install dependencies first so a source-only change does not re-fetch them.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web ./web
RUN cd web && npm run build

# The manual, which this image shipped without until now. `go:embed all:site`
# compiles against a directory holding one .gitkeep, so a build with no manual
# in it succeeds and the image serves "The Portico manual was not included in
# this build" at /docs — a correct sentence that nobody was meant to read.
FROM python:3.12-alpine AS manual
WORKDIR /src
COPY hack/docs-requirements.txt ./hack/
RUN pip install --no-cache-dir -r hack/docs-requirements.txt
# Only what the build reads, so editing a Go file does not reinstall mkdocs.
COPY mkdocs.yml ./
COPY mkdocs-overrides ./mkdocs-overrides
COPY docs ./docs
COPY hack/build-docs.sh ./hack/
RUN sh hack/build-docs.sh

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Both builds land in the packages that embed them.
COPY --from=web /src/internal/web/dist ./internal/web/dist
COPY --from=manual /src/internal/docs/site ./internal/docs/site
ARG VERSION=dev
# CGO is off because the sqlite driver is pure Go; that is what allows a
# scratch image with no libc.
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/Paraview-RD/portico/internal/server.Version=${VERSION}" \
    -o /portico ./cmd/server

FROM scratch

# Run unprivileged. scratch has no user database, so this is a bare uid:gid
# (nobody); /data below must be writable by it, which the volume mount is.
USER 65534:65534
# Root certificates, which this image did not have until webhooks existed.
#
# Portico makes outbound TLS connections in three places: delivering an event
# to a subscriber's https endpoint, sending mail over SMTP with STARTTLS or
# implicit TLS, and reading a directory over LDAPS or LDAP StartTLS. Without a
# CA bundle all three fail certificate verification inside the container while
# working perfectly on a developer's machine, which has a system store — the
# failure appears only in a deployment, and only in the features that are
# hardest to notice are broken.
#
# The bundle is copied rather than the image rebased, so this stays FROM
# scratch and stays one binary plus one file of certificates.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Time zone data is deliberately absent: without it the container reports UTC
# only, which is fine, and adding tzdata would mean leaving scratch.
# Timestamps are stored in UTC for this reason.
COPY --from=build /portico /portico

# No volume: state lives in PostgreSQL, so the container is disposable.
ENV PORTICO_ADDR=:8410

EXPOSE 8410

# The image has no shell, so the check has to be something the image already
# contains — which is this binary and nothing else. `portico ready` asks the
# running instance whether it can reach its database and exits 0 or 1.
#
# start-period is generous because the first start applies migrations, and a
# container reported unhealthy while it is legitimately still starting gets
# killed and restarted into the same delay.
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=3 \
    CMD ["/portico", "ready"]

ENTRYPOINT ["/portico"]
