FROM golang:1.26.4-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /src
ARG TARGETARCH=amd64
ARG VERSION=dev
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags netgo,osusergo \
      -ldflags="-s -w -buildid= -X main.version=${VERSION}" \
      -o /out/fragata ./cmd/fragata

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata ffmpeg \
    && addgroup -S -g 65532 fragata \
    && adduser -S -D -H -u 65532 -G fragata fragata \
    && mkdir -p /data /recordings \
    && chown -R 65532:65532 /data /recordings
COPY --from=builder /out/fragata /usr/local/bin/fragata
USER 65532:65532
VOLUME ["/data", "/recordings"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fragata"]
