terraform {
  required_providers {
    nerdctl = {
      source = "kmaris/nerdctl"
    }
  }
}

provider "nerdctl" {
  host = "ssh://user@admin0.ned.kmaris.net:22"
  sudo = true
}

resource "nerdctl_image" "traefik" {
  name = "traefik:v3"
}

resource "nerdctl_volume" "netbootxyz_config" {
  name = "netbootxyz_config"
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

  ports = [
    { internal = 80, external = 80 },
    { internal = 443, external = 443 },
  ]

  volumes = [
    { container_path = "/etc/traefik/dynamic", host_path = "/etc/traefik/dynamic", read_only = true },
  ]
}

resource "nerdctl_container" "netbootxyz" {
  name  = "netbootxyz"
  image = "ghcr.io/netbootxyz/netbootxyz"

  ports = [
    { internal = 69, external = 69, protocol = "udp" },
    { internal = 80, external = 8081 },
  ]

  volumes = [
    { container_path = "/config", volume_name = nerdctl_volume.netbootxyz_config.name },
  ]
}
