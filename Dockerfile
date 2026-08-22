FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ringring ./cmd/ringring

FROM debian:13-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 ringring \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin ringring

COPY --from=build /out/ringring /usr/local/bin/ringring
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/ringring/
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ringring"]
