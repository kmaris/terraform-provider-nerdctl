package nerdctl

import (
	"errors"
	"reflect"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	c, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.nerdctlPath != "nerdctl" {
		t.Errorf("nerdctlPath = %q, want %q", c.nerdctlPath, "nerdctl")
	}
	if c.namespace != "default" {
		t.Errorf("namespace = %q, want %q", c.namespace, "default")
	}
	if c.sshArgs != nil {
		t.Errorf("sshArgs = %v, want nil for local execution", c.sshArgs)
	}
}

func TestNewSSHHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		sshOpts []string
		want    []string
	}{
		{
			name: "host only",
			host: "ssh://box",
			want: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "box"},
		},
		{
			name: "user and port",
			host: "ssh://deploy@box:2222",
			want: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "-p", "2222", "deploy@box"},
		},
		{
			name:    "ssh_opts come before defaults so they win first-value option resolution",
			host:    "ssh://box",
			sshOpts: []string{"-i", "~/.ssh/deploy_key", "-o", "ConnectTimeout=5"},
			want: []string{
				"-i", "~/.ssh/deploy_key", "-o", "ConnectTimeout=5",
				"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "box",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(Config{Host: tt.host, SSHOpts: tt.sshOpts})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if !reflect.DeepEqual(c.sshArgs, tt.want) {
				t.Errorf("sshArgs = %v, want %v", c.sshArgs, tt.want)
			}
		})
	}
}

func TestNewRejectsNonSSH(t *testing.T) {
	if _, err := New(Config{Host: "tcp://box:2375"}); err == nil {
		t.Fatal("New accepted a tcp:// host, want error")
	}
}

func TestArgvLocal(t *testing.T) {
	c, _ := New(Config{})
	name, args := c.argv("ps", "-a")
	if name != "nerdctl" {
		t.Errorf("name = %q, want %q", name, "nerdctl")
	}
	want := []string{"--namespace", "default", "ps", "-a"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestArgvSudo(t *testing.T) {
	c, _ := New(Config{Sudo: true, Namespace: "prod", NerdctlPath: "/usr/local/bin/nerdctl"})
	name, args := c.argv("ps")
	if name != "sudo" {
		t.Errorf("name = %q, want %q", name, "sudo")
	}
	want := []string{"-n", "/usr/local/bin/nerdctl", "--namespace", "prod", "ps"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestArgvSSHQuotesArguments(t *testing.T) {
	c, err := New(Config{Host: "ssh://deploy@box", Sudo: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	name, args := c.argv("run", "--label", `note=it's a "test" $HOME`)
	if name != "ssh" {
		t.Errorf("name = %q, want %q", name, "ssh")
	}
	want := []string{
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "deploy@box",
		`PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin" ` +
			`'sudo' '-n' 'nerdctl' '--namespace' 'default' 'run' '--label' 'note=it'\''s a "test" $HOME'`,
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"has space", "'has space'"},
		{"it's", `'it'\''s'`},
		{"$VAR `cmd` \"quoted\"", "'$VAR `cmd` \"quoted\"'"},
		{"", "''"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestNotFound(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("exit status 1: time=... level=fatal msg=\"no such container: web\""), true},
		{errors.New("exit status 1: Error: No such image: traefik:v3"), true},
		{errors.New("volume web not found"), true},
		// A missing binary must not read as a missing object, or Read would
		// drop resources from state when the host is misconfigured.
		{errors.New("exit status 127: bash: line 1: nerdctl: command not found"), false},
		{errors.New(`exec: "nerdctl": executable file not found in $PATH`), false},
		{errors.New("permission denied"), false},
	}
	for _, tt := range tests {
		if got := NotFound(tt.err); got != tt.want {
			t.Errorf("NotFound(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
