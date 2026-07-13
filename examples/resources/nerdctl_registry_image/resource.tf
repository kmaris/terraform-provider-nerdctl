# Build an image under its registry reference, then push it.
resource "nerdctl_image" "app" {
  name = "registry.example.com/app:v1"

  build = {
    context = "/srv/app"
  }
}

resource "nerdctl_registry_image" "app" {
  name = nerdctl_image.app.name

  # Re-push whenever the local image changes.
  triggers = {
    image_id = nerdctl_image.app.id
  }
}
