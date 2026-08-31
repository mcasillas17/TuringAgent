module github.com/project-turing/mcp-files

go 1.25.0

require (
	github.com/mcasillas17/TuringAgent v0.0.0
	golang.org/x/sys v0.47.0
	golang.org/x/text v0.37.0
	google.golang.org/grpc v1.82.1
)

require (
	golang.org/x/net v0.55.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/mcasillas17/TuringAgent => ../..
