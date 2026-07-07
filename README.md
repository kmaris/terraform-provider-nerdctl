# terraform-provider-nerdctl

A Terraform provider that manages containers on [containerd](https://containerd.io/)
hosts by wrapping the [nerdctl](https://github.com/containerd/nerdctl) CLI,
locally or over ssh. Modeled on the resource shapes of the
[kreuzwerker/docker](https://registry.terraform.io/providers/kreuzwerker/docker/latest)
provider.

containerd has no remote-manageable API like the Docker Engine API — nerdctl
is the layer that provides networking (CNI), named volumes, restart policies,
and port publishing. This provider treats the nerdctl CLI as its contract and
shells out to it (via `ssh` for remote hosts), parsing `inspect` JSON for
reads.

**Status: MVP.** Containers are immutable — every attribute change forces a
replacement (the docker provider does this for most attributes too). Known
limitations:

- `command` and `entrypoint` drift is not detected: the OCI spec merges
  them, so neither can be recovered from inspect output. `workdir` is absent
  from inspect output entirely. `user` and `hostname` drift is tracked only
  when set in config (unset means image/runtime defaults, which inspect
  cannot distinguish from explicit values). Everything else (image, restart,
  networks, env, ports, labels, volumes, memory, cpus) is refreshed on read.
- Network `driver` drift is not detected (`network inspect` does not report
  it) and imported networks assume `bridge`.
- Remote hosts need non-interactive ssh (key auth in your agent) and, for
  rootful containerd as a non-root user, passwordless sudo (`sudo = true`).
- For rootless containerd on remote hosts, enable lingering
  (`loginctl enable-linger <user>`) so containerd outlives ssh sessions —
  otherwise containers stop when the last session closes and provider calls
  race containerd's startup. The provider appends the sbin dirs to the remote
  `PATH` (Debian-family non-interactive ssh omits them) so CNI can find
  iptables; a POSIX login shell is assumed on the remote host.

## Build and use locally

```sh
go install .
```

Then point Terraform at your `$GOBIN` with a `dev_overrides` block in
`~/.terraformrc` (no registry publish or `terraform init` lockfile needed):

```hcl
provider_installation {
  dev_overrides {
    "kmaris/nerdctl" = "/home/you/go/bin" # your $GOPATH/bin or $GOBIN
  }
  direct {}
}
```

Registry-style docs live in `docs/`, generated from schema descriptions and
the snippets under `examples/provider`, `examples/resources`, and
`examples/data-sources`. Regenerate after schema changes:

```sh
go tool tfplugindocs generate
```

## Provider configuration

```hcl
provider "nerdctl" {
  host         = "ssh://user@host:22" # omit to run nerdctl locally
  ssh_opts     = ["-i", "~/.ssh/deploy_key"] # extra ssh options (docker provider shape)
  sudo         = true                 # run nerdctl under `sudo -n`
  namespace    = "default"            # containerd namespace
  nerdctl_path = "nerdctl"            # binary path on the target host
}
```

ssh connections run in batch mode with a 30s connect timeout; entries in
`ssh_opts` take precedence over those defaults.

## Resources

### `nerdctl_image`

```hcl
resource "nerdctl_image" "traefik" {
  name = "traefik:v3"
}
```

### `nerdctl_volume`

```hcl
resource "nerdctl_volume" "config" {
  name = "app_config"
}
```

Exports `mountpoint`, the backing directory on the host.

### `nerdctl_network`

```hcl
resource "nerdctl_network" "app" {
  name    = "app-net"
  driver  = "bridge"        # default; also macvlan, ipvlan
  subnet  = "10.5.0.0/24"   # auto-assigned when unset
  gateway = "10.5.0.1"      # requires subnet

  labels = {
    "some.label" = "value"
  }
}
```

Exports `id`, the CNI network ID.

### `nerdctl_container`

```hcl
resource "nerdctl_container" "app" {
  name    = "app"
  image   = nerdctl_image.traefik.name
  restart = "unless-stopped" # default
  command = ["--flag=value"]

  entrypoint = "/bin/app"  # override image entrypoint binary
  user       = "1000:1000" # user[:group], image default when unset
  workdir    = "/srv"
  hostname   = "app-host"
  memory     = "512m" # docker-style size
  cpus       = 1.5    # cores

  networks = [nerdctl_network.app.name] # default bridge when unset

  env = {
    SOME_VAR = "value"
  }

  ports = [
    { internal = 80, external = 8080 },            # protocol defaults to tcp
    { internal = 69, external = 69, protocol = "udp" },
  ]

  labels = {
    "some.label" = "value"
  }

  volumes = [
    { container_path = "/data", volume_name = nerdctl_volume.config.name },
    { container_path = "/etc/app", host_path = "/srv/app", read_only = true },
  ]
}
```

Each `volumes` entry takes exactly one of `host_path` (bind mount) or
`volume_name` (named volume).

## Data sources

`nerdctl_image`, `nerdctl_volume`, and `nerdctl_network` read existing
objects by name, failing when absent:

```hcl
data "nerdctl_network" "bridge" {
  name = "bridge" # exports id, subnet, gateway, labels
}

data "nerdctl_image" "existing" {
  name = "nginx:alpine" # exports id (digest)
}

data "nerdctl_volume" "existing" {
  name = "app_config" # exports mountpoint
}
```

## Importing existing objects

All three resources import by name (the image reference for images):

```sh
terraform import nerdctl_image.traefik traefik:v3
terraform import nerdctl_volume.config app_config
terraform import nerdctl_network.app app-net
terraform import nerdctl_container.app app
```

Container import recovers every attribute except `command`, `entrypoint`,
`workdir`, `user`, and `hostname` (see limitations above) — if the container
was started with any of these, set them in config to match before the next
apply, or the plan will propose a replacement.
Anonymous volumes from image `VOLUME` directives are not imported; they are
image-implied, not configuration.

Note that Traefik's docker label discovery does not work against containerd —
there is no docker socket to watch. Labels are still applied to containers,
but route Traefik with its file provider instead (see `examples/main.tf`).
