FROM golang:1.24.2 AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOOS=linux \
    GOPROXY=https://proxy.golang.org,direct

WORKDIR /build
ADD go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "-s -w -X 'go-template/common/config.Version=$(cat VERSION)' -extldflags '-static'" -o go-template

FROM alpine:latest

RUN apk update && \
    apk upgrade && \
    apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates 2>/dev/null || true

COPY --from=builder /build/go-template /
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/go-template"]
