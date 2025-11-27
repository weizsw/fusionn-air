FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/fusionn-air/internal/version.Version=${VERSION}" \
    -o fusionn-air ./cmd/fusionn-air

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/fusionn-air .

ENV ENV=production
ENV CONFIG_PATH=/app/config/config.yaml

EXPOSE 8080

CMD ["./fusionn-air"]
