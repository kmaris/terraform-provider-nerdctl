action "nerdctl_container_restart" "app" {
  config {
    container = "app"
    timeout   = 5 # seconds before kill, 10 when unset
  }
}
