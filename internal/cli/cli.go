// Package cli implements the pbmac command-line surface. Commands emit
// human-readable output by default and JSON with --output json, so a GUI can
// drive the same binary (see docs/DESIGN.md §6).
package cli

import (
	"fmt"
	"os"
)

// Version is the client version, overridable at build time with -ldflags.
var Version = "0.0.1-dev"

// Run dispatches a command from os.Args-style arguments and returns an exit code.
func Run(args []string) int {
	if len(args) < 2 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[1], args[2:]
	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("pbmac %s\n", Version)
		return 0
	case "help", "--help", "-h":
		usage(os.Stdout)
		return 0
	case "backup":
		return cmdBackup(rest)
	case "restore":
		return cmdRestore(rest)
	case "ping":
		return cmdPing(rest)
	case "list", "snapshots":
		return cmdList(rest)
	case "login":
		return cmdLogin(rest)
	default:
		fmt.Fprintf(os.Stderr, "pbmac: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `pbmac — Proxmox Backup client for macOS

Usage:
  pbmac <command> [flags]

Commands:
  ping      Check connectivity/auth by fetching the server version
  backup    Back up a directory to a PBS datastore
  restore   List or restore files from a snapshot
  list      List snapshots in the datastore
  login     Store repository + fingerprint in the local config
  version   Print the client version
  help      Show this help

Repository & credentials (flag > env > config):
  --repo, PBS_REPOSITORY      [[user@realm]@host[:port]:]datastore
  PBS_API_TOKEN               USER@REALM!TOKENID:SECRET
  PBS_FINGERPRINT             expected server cert SHA-256

Run "pbmac <command> --help" for command-specific flags.
`)
}

// fail prints an error to stderr and returns a non-zero exit code.
func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "pbmac: "+format+"\n", a...)
	return 1
}
