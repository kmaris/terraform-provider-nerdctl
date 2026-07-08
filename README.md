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

Registry-style docs live in `docs/`, generated from schema descriptions,
the snippets under `examples/provider`, `examples/resources`, and
`examples/data-sources`, and the page templates in `templates/` (the
provider index and the rootless-host guide carry hand-written prose there).
Regenerate after schema or template changes:

```sh
go tool tfplugindocs generate
```

## Testing

`go test ./...` runs the unit tests (argument building, inspect parsing,
refresh semantics). The acceptance suite runs real plan/apply/destroy
cycles against a real containerd host via terraform-plugin-testing and is
gated behind `TF_ACC`:

```sh
NERDCTL_TEST_HOST=ssh://host TF_ACC=1 go test -v -run TestAcc ./internal/provider/ -timeout 30m
```

- Test objects use randomized `tfacc-*` names in a dedicated containerd
  namespace (`NERDCTL_TEST_NAMESPACE`, default `tfacc`), so they never mix
  with real workloads — but host port bindings (18080, 16969) and CNI
  networks are host-global.
- Leave `NERDCTL_TEST_HOST` empty to run against local nerdctl.
- The Terraform CLI is taken from `TF_ACC_TERRAFORM_PATH`,
  `TF_ACC_TERRAFORM_VERSION`, or `PATH`, and auto-downloaded otherwise.
  The actions test skips itself below Terraform 1.14.
- `nerdctl_system_prune` is not acceptance-tested: unused-network pruning
  reaches across containerd namespaces. Its arguments are unit-tested.
- Known leftovers: the actions test writes tarballs under `/tmp` on the
  target host that the suite cannot remove.

## CI and releases

Pushes and pull requests run build, vet, gofmt, tests, and a docs-freshness
gate (regenerate + diff) in GitHub Actions. A separate Acceptance workflow
runs the full `TF_ACC` suite against real containerd on the runner itself,
in both rootless mode (set up per the rootless-host guide) and rootful mode
(against the runner's existing containerd, as root). Pushing a `v*` tag runs
goreleaser: multi-platform zips, a GPG-signed `SHA256SUMS`, and the registry
manifest attached to the GitHub release. Two repository secrets are
required for releases: `GPG_PRIVATE_KEY` (ASCII-armored signing key) and
`PASSPHRASE`. To publish on the Terraform Registry, add the key's public
half to the registry account and publish the repository; tagged releases
are then ingested automatically.

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

  dns        = ["1.1.1.1"]          # host resolver config when unset
  dns_opts   = ["ndots:2"]
  dns_search = ["example.internal"]

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

## Actions

Imperative operations (Terraform 1.14+), mirroring the docker provider's
action set: `nerdctl_exec`, `nerdctl_container_export`,
`nerdctl_image_import`, `nerdctl_image_load`, `nerdctl_image_save`, and
`nerdctl_system_prune`. Trigger them from a resource lifecycle:

```hcl
resource "nerdctl_container" "app" {
  # ...
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.nerdctl_exec.mark_ready]
    }
  }
}

action "nerdctl_exec" "mark_ready" {
  config {
    container = nerdctl_container.app.name
    command   = ["sh", "-c", "echo ready > /srv/status"]
  }
}
```

Two caveats: file paths in the archive actions (`image_save`, `image_load`,
`image_import`, `container_export`) are on the **target host**, not the
machine running Terraform; and `system_prune` is destructive to objects
Terraform does not manage.

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
