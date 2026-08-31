FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:3.20
RUN addgroup -g 1000 -S mcp-system \
    && adduser -u 1000 -S -G mcp-system mcp-system
WORKDIR /app
COPY --from=builder /app/server ./server
USER mcp-system:mcp-system
EXPOSE 7100
CMD ["./server"]
