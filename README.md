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

- No drift detection on container attributes; `Read` only checks existence.
- No `terraform import` support yet.
- No `nerdctl_network` resource yet (containers run on the default bridge).
- Remote hosts need non-interactive ssh (key auth in your agent) and, for
  rootful containerd as a non-root user, passwordless sudo (`sudo = true`).

## Build and use locally

```sh
go install .
```

Then point Terraform at your `$GOBIN` with a `dev_overrides` block in
`~/.terraformrc` (no registry publish or `terraform init` lockfile needed):

```hcl
provider_installation {
  dev_overrides {
    "kmaris/nerdctl" = "/Users/kmaris/go/bin"
  }
  direct {}
}
```

## Provider configuration

```hcl
provider "nerdctl" {
  host         = "ssh://user@host:22" # omit to run nerdctl locally
  sudo         = true                 # run nerdctl under `sudo -n`
  namespace    = "default"            # containerd namespace
  nerdctl_path = "nerdctl"            # binary path on the target host
}
```

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

### `nerdctl_container`

```hcl
resource "nerdctl_container" "app" {
  name    = "app"
  image   = nerdctl_image.traefik.name
  restart = "unless-stopped" # default
  command = ["--flag=value"]

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

Note that Traefik's docker label discovery does not work against containerd —
there is no docker socket to watch. Labels are still applied to containers,
but route Traefik with its file provider instead (see `examples/main.tf`).
