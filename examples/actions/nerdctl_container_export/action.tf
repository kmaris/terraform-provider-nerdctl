action "nerdctl_container_export" "backup" {
  config {
    container = "app"
    output    = "/var/backups/app.tar" # written on the target host
  }
}
