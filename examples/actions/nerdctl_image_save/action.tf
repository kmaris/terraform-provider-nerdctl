action "nerdctl_image_save" "archive" {
  config {
    images = ["traefik:v3", "nginx:alpine"]
    output = "/srv/images/bundle.tar" # written on the target host
  }
}
