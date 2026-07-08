action "nerdctl_image_import" "restore" {
  config {
    input     = "/var/backups/app.tar" # path or URL, resolved on the target host
    reference = "app:restored"
  }
}
