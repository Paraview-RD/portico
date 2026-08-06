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
    -ldflags "-s -w -X github.com/paraview/keylite/internal/server.Version=${VERSION}" \
    -o /keylite ./cmd/server

FROM scratch
# Certificates are not needed today (no outbound TLS), but time zone data is:
# without it the container reports UTC only, which is fine, and adding tzdata
# would mean leaving scratch. Timestamps are stored in UTC for this reason.
COPY --from=build /keylite /keylite

# The database is a file; mount a volume here to keep it across restarts.
VOLUME ["/data"]
ENV KEYLITE_DB_DSN=/data/keylite.db
ENV KEYLITE_ADDR=:8410

EXPOSE 8410
ENTRYPOINT ["/keylite"]
