// Package nerdctl executes nerdctl commands locally or on a remote host over
// ssh. There is no daemon API to speak to (containerd's gRPC socket is
// local-only), so the CLI is the contract, in the same way the kreuzwerker
// docker provider's contract is the Docker Engine API.
package nerdctl

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Config struct {
	// Host is empty for local execution, or "ssh://[user@]host[:port]".
	Host string
	// NerdctlPath is the nerdctl binary on the target host. Defaults to "nerdctl".
	NerdctlPath string
	// Namespace is the containerd namespace. Defaults to "default".
	Namespace string
	// Sudo prepends "sudo -n" for rootful containerd as a non-root user.
	Sudo bool
}

type Client struct {
	sshArgs     []string // nil means run locally
	nerdctlPath string
	namespace   string
	sudo        bool
}

func New(cfg Config) (*Client, error) {
	c := &Client{
		nerdctlPath: cfg.NerdctlPath,
		namespace:   cfg.Namespace,
		sudo:        cfg.Sudo,
	}
	if c.nerdctlPath == "" {
		c.nerdctlPath = "nerdctl"
	}
	if c.namespace == "" {
		c.namespace = "default"
	}

	if cfg.Host != "" {
		u, err := url.Parse(cfg.Host)
		if err != nil {
			return nil, fmt.Errorf("parsing host %q: %w", cfg.Host, err)
		}
		if u.Scheme != "ssh" {
			return nil, fmt.Errorf("host %q: only ssh:// hosts are supported", cfg.Host)
		}
		c.sshArgs = []string{"-o", "BatchMode=yes"}
		if p := u.Port(); p != "" {
			c.sshArgs = append(c.sshArgs, "-p", p)
		}
		target := u.Hostname()
		if user := u.User.Username(); user != "" {
			target = user + "@" + target
		}
		c.sshArgs = append(c.sshArgs, target)
	}

	return c, nil
}

// Run executes nerdctl with the given arguments and returns trimmed stdout.
// Failures include stderr in the error message.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	full := make([]string, 0, len(args)+4)
	if c.sudo {
		full = append(full, "sudo", "-n")
	}
	full = append(full, c.nerdctlPath, "--namespace", c.namespace)
	full = append(full, args...)

	var cmd *exec.Cmd
	if c.sshArgs != nil {
		// The remote shell re-splits the command line, so quote each argument.
		quoted := make([]string, len(full))
		for i, a := range full {
			quoted[i] = shellQuote(a)
		}
		sshArgs := append(append([]string{}, c.sshArgs...), strings.Join(quoted, " "))
		cmd = exec.CommandContext(ctx, "ssh", sshArgs...)
	} else {
		cmd = exec.CommandContext(ctx, full[0], full[1:]...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// NotFound reports whether an error from Run looks like a missing-object
// error, so callers can distinguish "gone" from "broken".
func NotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such") || strings.Contains(msg, "not found")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
