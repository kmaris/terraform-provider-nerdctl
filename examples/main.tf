terraform {
  required_providers {
    nerdctl = {
      source = "kmaris/nerdctl"
    }
  }
}

# Rootless containerd on a remote host, connecting as the default ssh user.
# Requires lingering on the host (`loginctl enable-linger <user>`) so
# containerd survives ssh sessions and reboots.
#
# Set the real host in an untracked examples/local.auto.tfvars:
#   host = "ssh://your-host.example.com"
variable "host" {
  description = "Remote host to run nerdctl on, as ssh://[user@]host[:port]. Set \"\" to run nerdctl locally."
  type        = string
  default     = "ssh://containers.example.com"
}

provider "nerdctl" {
  host = var.host
}

resource "nerdctl_image" "traefik" {
  name = "traefik:v3"
}

resource "nerdctl_volume" "traefik_config" {
  name = "traefik_config"
}

resource "nerdctl_container" "traefik" {
  name  = "traefik"
  image = nerdctl_image.traefik.name

  command = [
    "--providers.file.directory=/etc/traefik/dynamic",
    "--providers.file.watch=true",
    "--entrypoints.web.address=:80",
    "--entrypoints.websecure.address=:443",
  ]

  # Traefik also reads TRAEFIK_* static config from the environment; after
  # apply, a re-plan must show no env drift.
  env = {
    TRAEFIK_LOG_LEVEL = "INFO"
    TZ                = "UTC"
  }

  ports = [
    { internal = 80, external = 8080 },
    { internal = 443, external = 8443 },
  ]

  volumes = [
    { container_path = "/etc/traefik/dynamic", volume_name = nerdctl_volume.traefik_config.name, read_only = true },
  ]
}

