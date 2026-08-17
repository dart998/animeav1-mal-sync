# syntax=docker/dockerfile:1
FROM python:3.13-alpine AS patch
WORKDIR /src
COPY main.go VERSION docker-compose.portainer.yml ./
COPY tools/integrate_reverse_rc.py ./tools/integrate_reverse_rc.py
RUN python3 tools/integrate_reverse_rc.py

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY --from=patch /src/main.go ./main.go
COPY reverse_sync.go reverse_conflicts.go reverse_runtime.go ./
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN gofmt -w *.go && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /out/animeav1-mal-sync .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/animeav1-mal-sync /animeav1-mal-sync
EXPOSE 8787
VOLUME ["/data"]
ENTRYPOINT ["/animeav1-mal-sync"]
