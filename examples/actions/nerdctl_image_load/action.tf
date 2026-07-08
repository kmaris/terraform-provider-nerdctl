action "nerdctl_image_load" "preload" {
  config {
    input = "/srv/images/app.tar" # read from the target host
  }
}
