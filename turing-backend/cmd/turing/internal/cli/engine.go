package cli

import "context"

// engine runs `turing engine <subcommand>`. A subcommand it does not know is
// not repeated back, in case it is a secret typed in the wrong place.
func (e Env) engine(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return e.usageError("turing engine needs a subcommand; run ./turing help")
	}
	switch args[0] {
	case "detect":
		return e.engineDetect(ctx, args[1:])
	default:
		return e.usageError("unknown engine command; run ./turing help")
	}
}
