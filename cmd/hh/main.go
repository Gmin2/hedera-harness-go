package main

import (
	"os"

	"github.com/Gmin2/hedera-harness-go/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
