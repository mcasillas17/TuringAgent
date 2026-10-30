// Command turing operates the Turing server from a terminal on the machine it
// runs on. Run it through turing-backend/turing, which builds it and passes
// the backend directory.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run requires --backend-dir <absolute path> as the first two arguments, which
// the wrapper always passes, and runs the command that follows.
func run(args []string, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "--backend-dir" || !filepath.IsAbs(args[1]) {
		_, _ = fmt.Fprintf(stderr, "turing needs --backend-dir <absolute path> as its first two arguments, which ./turing passes; run it as ./turing instead.\n\n%s", cli.Usage)
		return 2
	}
	return cli.Main(args[2:], cli.OSEnv(args[1]))
}
