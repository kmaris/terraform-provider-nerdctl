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
	"os"
	"os/exec"
	"path"
	"strings"
)

// Config describes how to reach a containerd host and how to invoke
// nerdctl on it.
type Config struct {
	// Host is empty for local execution, or "ssh://[user@]host[:port]".
	Host string
	// SSHOpts are extra ssh CLI options for remote hosts, e.g.
	// []string{"-i", "~/.ssh/deploy_key", "-J", "bastion"}. Mirrors the
	// kreuzwerker/docker provider's ssh_opts.
	SSHOpts []string
	// NerdctlPath is the nerdctl binary on the target host. Defaults to "nerdctl".
	NerdctlPath string
	// Namespace is the containerd namespace. Defaults to "default".
	Namespace string
	// Sudo prepends "sudo -n" for rootful containerd as a non-root user.
	Sudo bool
}

// Client runs nerdctl commands against a host, locally or over ssh.
type Client struct {
	sshArgs     []string // nil means run locally
	nerdctlPath string
	namespace   string
	sudo        bool
}

// New builds a Client from cfg, validating the host URL.
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
		// User options come first: ssh uses the first value it sees for an
		// -o option, so ssh_opts can override the defaults below.
		c.sshArgs = append(c.sshArgs, cfg.SSHOpts...)
		c.sshArgs = append(c.sshArgs, "-o", "BatchMode=yes", "-o", "ConnectTimeout=30")
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

// argv returns the executable and argument list Run will execute for the
// given nerdctl arguments, after sudo, namespace, and ssh wrapping.
func (c *Client) argv(args ...string) (string, []string) {
	full := make([]string, 0, len(args)+4)
	if c.sudo {
		full = append(full, "sudo", "-n")
	}
	full = append(full, c.nerdctlPath, "--namespace", c.namespace)
	full = append(full, args...)

	if c.sshArgs != nil {
		// The remote shell re-splits the command line, so quote each argument.
		quoted := make([]string, len(full))
		for i, a := range full {
			quoted[i] = shellQuote(a)
		}
		// Non-interactive ssh PATHs often lack the sbin dirs (Debian-family
		// defaults) where iptables lives, which nerdctl needs for CNI. The
		// POSIX prefix assignment scopes the fix to this command.
		remote := `PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin" ` + strings.Join(quoted, " ")
		return "ssh", append(append([]string{}, c.sshArgs...), remote)
	}
	return full[0], full[1:]
}

// Run executes nerdctl with the given arguments and returns trimmed stdout.
// Failures include stderr in the error message.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	name, argv := c.argv(args...)
	cmd := exec.CommandContext(ctx, name, argv...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(argv, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// WriteFile writes content to filePath on the target host, creating any
// missing parent directories. It writes to the local filesystem when nerdctl
// runs locally, and streams the content over ssh otherwise, so callers can
// stage files (such as compose files) on a remote host.
func (c *Client) WriteFile(ctx context.Context, filePath, content string) error {
	dir := path.Dir(filePath)
	if c.sshArgs == nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", filePath, err)
		}
		return nil
	}

	remote := fmt.Sprintf("mkdir -p %s && cat > %s", shellQuote(dir), shellQuote(filePath))
	args := append(append([]string{}, c.sshArgs...), remote)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(content)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing %s over ssh: %w: %s", filePath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// NotFound reports whether an error from Run looks like a missing-object
// error, so callers can distinguish "gone" from "broken". A missing nerdctl
// binary also says "not found" but means the host is broken, not the object —
// treating it as gone would silently drop resources from state.
func NotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "command not found") || strings.Contains(msg, "executable file not found") {
		return false
	}
	// Missing networks phrase it differently from the "no such X" /
	// "X not found" wording of containers, images, and volumes.
	return strings.Contains(msg, "no such") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no network found") ||
		strings.Contains(msg, "unable to find any network")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
