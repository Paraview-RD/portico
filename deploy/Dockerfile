# Build the frontend, then the binary that embeds it, then ship only the
# binary. The result is a scratch-based image containing one file.

FROM node:22-alpine AS web
WORKDIR /src
# Install dependencies first so a source-only change does not re-fetch them.
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web ./web
RUN cd web && npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The frontend build lands in the package that embeds it.
COPY --from=web /src/internal/web/dist ./internal/web/dist
ARG VERSION=dev
# CGO is off because the sqlite driver is pure Go; that is what allows a
# scratch image with no libc.
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/paraview/portico/internal/server.Version=${VERSION}" \
    -o /portico ./cmd/server

FROM scratch

# Run unprivileged. scratch has no user database, so this is a bare uid:gid
# (nobody); /data below must be writable by it, which the volume mount is.
USER 65534:65534
# Certificates are not needed today (no outbound TLS), but time zone data is:
# without it the container reports UTC only, which is fine, and adding tzdata
# would mean leaving scratch. Timestamps are stored in UTC for this reason.
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
