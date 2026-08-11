FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
COPY turing-backend ./turing-backend
RUN CGO_ENABLED=0 go build -tags sqlite_fts5 -o /out/turing-agent-runtime-go ./turing-backend/agent-runtime-go/cmd/runtime

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/turing-agent-runtime-go /app/turing-agent-runtime-go
ENTRYPOINT ["/app/turing-agent-runtime-go"]
