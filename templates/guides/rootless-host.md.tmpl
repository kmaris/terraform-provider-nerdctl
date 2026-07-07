---
page_title: "Preparing a rootless containerd host"
subcategory: ""
description: |-
  Host setup for managing rootless containerd with this provider:
  lingering, iptables, cgroup delegation, and privileged ports.
---

# Preparing a rootless containerd host

Rootless containerd lets this provider manage containers over ssh without
`sudo` — the containerd daemon, CNI networking, and all container state run
as an unprivileged user. The pieces below are one-time host setup; each one
was learned the hard way, so the *why* is included.

## 1. Install rootless containerd

With containerd and nerdctl installed (the
[nerdctl full release](https://github.com/containerd/nerdctl/releases)
bundles everything including CNI plugins and rootlesskit):

```sh
containerd-rootless-setuptool.sh install
```

This creates a user-level `containerd.service` systemd unit.

## 2. Enable lingering — required

```sh
sudo loginctl enable-linger <user>
```

Without lingering, the user's systemd instance — and rootless containerd
with it — starts when a session opens and stops when the last ssh session
closes. The consequences for this provider are severe: provider calls race
containerd's startup (`stat .../child_pid: no such file or directory`
failures), and containers stop entirely when nobody is logged in, defeating
restart policies. With lingering enabled, containerd starts at boot and
stays up.

Verify:

```sh
loginctl show-user <user> -p Linger   # Linger=yes
systemctl --user is-active containerd.service
```

## 3. Ensure iptables is installed

CNI's bridge networking shells out to `iptables`, which minimal server
installs may lack:

```sh
sudo apt install iptables   # or the distro equivalent
```

The provider appends `/usr/local/sbin:/usr/sbin:/sbin` to the remote
command's `PATH` (Debian-family non-interactive ssh omits them), but the
binary itself has to exist.

## 4. cgroup v2 delegation — for `memory` and `cpus` limits

Resource limits on rootless containers require the user's systemd slice to
be delegated the relevant controllers:

```sh
sudo mkdir -p /etc/systemd/system/user@.service.d
cat <<'EOF' | sudo tee /etc/systemd/system/user@.service.d/delegate.conf
[Service]
Delegate=cpu cpuset io memory pids
EOF
sudo systemctl daemon-reload
```

Log out and back in (or reboot) so the user manager picks it up. Without
delegation, `nerdctl run --memory ...` fails or silently ignores the limit.

## 5. Ports below 1024 — optional

An unprivileged user cannot bind host ports below 1024, so
`ports = [{ internal = 80, external = 80 }]` fails on a stock host. Either
publish on high ports, or lower the threshold:

```sh
echo 'net.ipv4.ip_unprivileged_port_start=0' | sudo tee /etc/sysctl.d/99-rootless-ports.conf
sudo sysctl --system
```

## Provider configuration

With the host prepared, no `sudo` is needed:

```terraform
provider "nerdctl" {
  host = "ssh://containers.example.com"
}
```

## Verifying the host

```sh
ssh <user>@<host> 'systemctl --user is-active containerd.service && nerdctl info >/dev/null && echo READY'
```

If this prints `READY` from a fresh connection (no other sessions open),
every piece above is in place.
