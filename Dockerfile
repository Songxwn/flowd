FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY . .
ARG VERSION=dev
RUN go mod tidy \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/flowd ./cmd/flowd

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/flowd /usr/local/bin/flowd

EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/flowd"]
