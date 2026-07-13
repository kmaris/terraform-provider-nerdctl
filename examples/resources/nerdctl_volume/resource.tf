resource "nerdctl_volume" "config" {
  name = "app_config"

  labels = {
    "some.label" = "value"
  }
}
