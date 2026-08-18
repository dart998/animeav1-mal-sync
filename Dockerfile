# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY tools/build_rc4.py /tmp/build_rc4.py
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN apk add --no-cache python3 \
    && python3 /tmp/build_rc4.py \
    && gofmt -w *.go \
    && go test ./... \
    && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
       go build -trimpath -ldflags="-s -w" -o /out/animeav1-mal-sync .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/animeav1-mal-sync /animeav1-mal-sync
EXPOSE 8787
VOLUME ["/data"]
ENTRYPOINT ["/animeav1-mal-sync"]
