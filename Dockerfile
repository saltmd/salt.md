# --- Frontend build (its output is the same for every target arch) ---
FROM --platform=$BUILDPLATFORM node:20-alpine AS web
ARG VERSION=dev
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci || npm install
COPY web ./
# `npm run build` runs the whole gate first, and part of that gate reads files
# OUTSIDE web/: check-wiki.mjs compares the wiki against the MCP catalogue and
# the route table. Without these two the image build died on a missing
# directory — the gate is deliberately not skippable, so what it needs has to
# be here.
#
# The cost is honest and worth naming: a change under server/ now busts this
# layer's cache and the frontend rebuilds. That is about thirty seconds on a
# build that only runs on a tag, and the alternative is a check that silently
# does not run in one of the two places we build.
COPY wiki /src/wiki
COPY server /src/server
# Same VERSION the backend is stamped with, so the two never disagree and the
# "reload" banner only fires after an actual deploy.
RUN SALT_VERSION=${VERSION} npm run build

# --- Backend build (cross-compiled to the requested target arch) ---
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
COPY --from=web /src/web/dist ./web/dist
# CGO off + pure-Go SQLite ⇒ a fully static binary, and building on the native
# builder while targeting $TARGETARCH avoids slow QEMU emulation of the compiler.
RUN go mod tidy && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X salt/server.Version=${VERSION}" -o /salt .

# --- Runtime ---
FROM alpine:3.20
RUN adduser -D -H salt && mkdir -p /data && chown salt /data
USER salt
ENV SALT_ADDR=:8420 SALT_DATA=/data
VOLUME /data
EXPOSE 8420
COPY --from=build /salt /usr/local/bin/salt
ENTRYPOINT ["salt"]
