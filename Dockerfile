# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY VERSION ./
COPY *.go ./
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN VERSION="$(tr -d '[:space:]' < VERSION)" && \
    sed -i "s/appVersion = \"[^\"]*\"/appVersion = \"${VERSION}\"/" main.go && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /out/animeav1-mal-sync .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/animeav1-mal-sync /animeav1-mal-sync
EXPOSE 8787
VOLUME ["/data"]
ENTRYPOINT ["/animeav1-mal-sync"]
