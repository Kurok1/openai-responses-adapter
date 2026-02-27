# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.24.1-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" \
  -o /out/adapter ./cmd/adapter

FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app && apk add --no-cache ca-certificates

COPY --from=builder /out/adapter /usr/local/bin/adapter

USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/adapter"]
