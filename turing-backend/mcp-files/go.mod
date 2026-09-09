module github.com/project-turing/mcp-files

go 1.25.0

require (
	github.com/mcasillas17/TuringAgent v0.0.0
	golang.org/x/sys v0.47.0
	golang.org/x/text v0.41.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/mattn/go-sqlite3 v1.14.50 // indirect
	github.com/oklog/ulid/v2 v2.1.2 // indirect
	golang.org/x/net v0.58.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/mcasillas17/TuringAgent => ../..
