# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o drakkar .

# Final minimal production image (~12MB)
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /
COPY --from=builder /app/drakkar /drakkar

# Run as non-root user for security
RUN addgroup -S drakkar && adduser -S drakkar -G drakkar
USER drakkar

ENTRYPOINT ["/drakkar"]
