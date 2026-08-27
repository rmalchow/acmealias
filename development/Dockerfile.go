# Development image for the Go service: the toolchain plus air, nothing else.
#
# The source is bind-mounted at run time (see compose.yaml), never copied in —
# this image exists only so `air` has a Go to call, and so that a container
# starts by COMPILING the service rather than by installing its own toolchain
# first.
#
# AIR IS PINNED, NOT @latest. `@latest` forces a network round trip on every
# build even when the module cache is warm, and lets each developer's toolchain
# drift independently — so a rebuild on one machine picks up a new air with new
# behaviour while another machine keeps the old one, and the difference shows up
# as "works for me". Bump this deliberately.
#
# WHERE air LANDS MATTERS. The default GOBIN is /go/bin, and compose bind-mounts
# ./data/go_mod at /go/pkg/mod — only the module cache, so /go/bin survives today.
# Installing to /usr/local/bin removes the dependency on that staying true: if
# anyone ever mounts the whole of /go (which is the obvious way to cache more),
# the image's air would vanish and the container would fail with `air: not found`
# on a fresh checkout, or silently run a stale one that was left in the volume.
#
# CGO_ENABLED=0 because every dependency here is pure Go (pgx, miekg/dns, gorm's
# postgres driver), so there is no reason to install gcc and musl-dev on top of
# alpine. It is set again in compose.yaml so a rebuild of the binary after an
# image change cannot silently pick up cgo.
FROM golang:alpine

ENV CGO_ENABLED=0
ENV GOBIN=/usr/local/bin
ENV PATH=/usr/local/bin:$PATH

# The container runs as the invoking developer, who has no home directory inside
# it. Go derives GOCACHE from $HOME and lands on /.cache/go-build, which a
# non-root user cannot create — the build then fails with `permission denied` at
# the first compile, long after the container looks healthy. Naming both caches
# explicitly removes the dependency on HOME entirely, and the paths are what
# compose mounts.
ENV GOCACHE=/gocache
ENV GOMODCACHE=/go/pkg/mod
ENV HOME=/tmp

RUN go install github.com/air-verse/air@v1.67.4

# curl is what a compose healthcheck shells out to, and wget-in-busybox is not
# a substitute for one that has to send a header.
RUN apk add --no-cache curl

WORKDIR /src
CMD ["air", "-c", ".air.toml"]
