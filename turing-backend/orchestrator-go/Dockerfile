FROM golang:1.23-bookworm AS build
ARG GOFLAGS=-tags=sqlite_fts5
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev sqlite3 libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
COPY turing-backend ./turing-backend
RUN CGO_ENABLED=1 go build $GOFLAGS -o /out/turing-orchestrator-go ./turing-backend/orchestrator-go/cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates sqlite3 libsqlite3-0 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/turing-orchestrator-go /app/turing-orchestrator-go
EXPOSE 3000 3001
ENTRYPOINT ["/app/turing-orchestrator-go"]
