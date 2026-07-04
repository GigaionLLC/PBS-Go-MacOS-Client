// Command pbmac is a native macOS (arm64) Proxmox Backup client. See the
// repository README and docs/DESIGN.md for architecture and protocol notes.
package main

import (
	"os"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
