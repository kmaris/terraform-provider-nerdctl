# Pull an image from a registry.
resource "nerdctl_image" "traefik" {
  name         = "traefik:v3"
  keep_locally = true
}

# Build an image from a Dockerfile on the host. Requires a running buildkitd.
resource "nerdctl_image" "app" {
  name = "app:dev"

  build = {
    context = "/srv/app"

    build_args = {
      VERSION = "1.2.3"
    }
  }

  # Sources are not tracked; force a rebuild when they change.
  triggers = {
    dockerfile_sha1 = filesha1("/srv/app/Dockerfile")
  }
}
