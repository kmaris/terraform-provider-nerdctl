# Track a remote tag and re-pull the local image when it moves.
data "nerdctl_registry_image" "upstream" {
  name = "traefik:v3"
}

resource "nerdctl_image" "traefik" {
  name = "traefik:v3"

  triggers = {
    digest = data.nerdctl_registry_image.upstream.sha256_digest
  }
}
