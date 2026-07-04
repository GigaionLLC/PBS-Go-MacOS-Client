//go:build darwin

package keychain

import (
	"fmt"
	"os/exec"
	"strings"
)

// Store saves (or updates, via -U) a generic password.
func Store(account, secret string) error {
	cmd := exec.Command("security", "add-generic-password",
		"-a", account, "-s", Service, "-w", secret, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain store: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Retrieve fetches a stored secret, or ErrNotFound.
func Retrieve(account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", account, "-s", Service, "-w").Output()
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Delete removes a stored secret (no error if absent).
func Delete(account string) error {
	_ = exec.Command("security", "delete-generic-password",
		"-a", account, "-s", Service).Run()
	return nil
}
