# syntax=docker/dockerfile:1

# --- Stage 1: build BOTH consoles (Angular) ----------------------------
# `internal/webui` embeds `all:dist` and subs into dist/console AND dist/admin
# (design D9), so both have to be built here. Building only one is a silent
# failure rather than a loud one: `go:embed all:dist` still succeeds, the
# fs.Sub for the missing surface returns an error that webui.go deliberately
# drops, and the published image serves 404 for every page of a console it
# claims to carry.
#
# Their configured outputPaths (frontend/angular.json) write into
# backend/internal/webui/dist/{console,admin}, so they must land there before
# the Go stage runs.
FROM node:22-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npx ng build console && npx ng build admin

# --- Stage 2: build the Go binary ---------------------------------------
FROM golang:1.26.3-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/backend/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o /out/acmeservice ./cmd/acmeservice
# CAP_NET_BIND_SERVICE lets the unprivileged process bind port 53 (DNS)
# without running as root or `--privileged` (design D9/D15). setcap here
# survives the COPY --from into the final stage below.
RUN apt-get update && apt-get install -y --no-install-recommends libcap2-bin \
	&& rm -rf /var/lib/apt/lists/* \
	&& setcap 'cap_net_bind_service=+ep' /out/acmeservice

# --- Stage 3: minimal runtime image --------------------------------------
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=backend /out/acmeservice /usr/local/bin/acmeservice
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/acmeservice"]
