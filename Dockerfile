FROM golang:1.26.4-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /src
ARG TARGETARCH=amd64
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN mkdir -p /empty-data && touch /empty-data/.keep && \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags netgo,osusergo \
      -ldflags="-s -w -buildid=" \
      -o /out/fragata ./cmd/fragata

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder --chown=65532:65532 /empty-data/.keep /data/.keep
COPY --from=builder /out/fragata /fragata
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/fragata"]
