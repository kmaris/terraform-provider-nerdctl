action "nerdctl_container_stop" "app" {
  config {
    container = "app"
    timeout   = 5 # seconds before kill, 10 when unset
  }
}
