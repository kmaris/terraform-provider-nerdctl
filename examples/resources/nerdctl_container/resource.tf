resource "nerdctl_container" "app" {
  name    = "app"
  image   = nerdctl_image.traefik.name
  restart = "unless-stopped" # default
  command = ["--flag=value"]

  entrypoint = "/bin/app"  # override image entrypoint binary
  user       = "1000:1000" # user[:group], image default when unset
  workdir    = "/srv"
  hostname   = "app-host"
  memory     = "512m" # docker-style size
  cpus       = 1.5    # cores

  networks = [nerdctl_network.app.name] # default bridge when unset

  env = {
    SOME_VAR = "value"
  }

  ports = [
    { internal = 80, external = 8080 },            # protocol defaults to tcp
    { internal = 69, external = 69, protocol = "udp" },
  ]

  labels = {
    "some.label" = "value"
  }

  volumes = [
    { container_path = "/data", volume_name = nerdctl_volume.config.name },
    { container_path = "/etc/app", host_path = "/srv/app", read_only = true },
  ]
}
