FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates sqlite-libs su-exec && \
    adduser -D -u 1001 appuser

WORKDIR /app
COPY --from=builder /build/server .
COPY static/ ./static/

RUN mkdir -p /app/data && chown appuser:appuser /app/data

COPY entrypoint.sh ./
RUN chmod +x entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
