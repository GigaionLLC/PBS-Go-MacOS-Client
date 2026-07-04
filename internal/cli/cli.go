// Package cli implements the pbmac command-line surface. Commands emit
// human-readable output by default and machine-readable JSON with --json, so a
// GUI can drive the same binary (see docs/CLI-JSON.md and docs/DESIGN.md §6).
package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// jsonMode, set by each command from its --json flag, makes fail() emit a
// machine-readable error envelope instead of a human line.
var jsonMode bool

// hasJSONFlag reports whether --json/-json appears in args (used by commands,
// like version, that don't run through a flag.FlagSet).
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" {
			return true
		}
	}
	return false
}

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
		if hasJSONFlag(rest) {
			b, _ := json.Marshal(map[string]string{"name": "pbmac", "version": Version})
			fmt.Println(string(b))
		} else {
			fmt.Printf("pbmac %s\n", Version)
		}
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
	case "archives":
		return cmdArchives(rest)
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
  archives  List the archives/files in a snapshot (its manifest)
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

// fail prints an error to stderr and returns exit code 1. Under --json it emits
// a stable {"error": "..."} envelope so a GUI can surface failures as data.
func fail(format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	if jsonMode {
		b, _ := json.Marshal(map[string]string{"error": msg})
		fmt.Fprintln(os.Stderr, string(b))
	} else {
		fmt.Fprintln(os.Stderr, "pbmac: "+msg)
	}
	return 1
}
