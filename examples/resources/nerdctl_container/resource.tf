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

  cap_add  = ["net_admin"] # without the CAP_ prefix
  cap_drop = ["mknod"]

  read_only    = true                  # root filesystem; use tmpfs/volumes for writable paths
  security_opt = ["no-new-privileges"] # also seccomp=<file>, apparmor=<profile>, ...
  group_add    = ["video"]             # extra groups, by name or GID
  shm_size     = "128m"                # /dev/shm, 64m when unset
  pid          = "host"                # or container:<name>
  init         = true                  # needs an init binary (tini) on the host
  stop_signal  = "SIGQUIT"             # SIGTERM when unset
  stop_timeout = 5                     # seconds before the runtime kills it
  platform     = "linux/amd64"         # use the normalized os/arch form

  devices = [
    { host_path = "/dev/fuse" }, # container_path defaults to host_path, permissions to rwm
  ]

  ulimits = [
    { name = "nofile", soft = 1024, hard = 2048 },
  ]

  sysctls = {
    "net.core.somaxconn" = "1024"
  }

  tmpfs = {
    "/run" = "size=64m" # nerdctl always adds noexec,nosuid,nodev
  }

  log_driver = "json-file" # default
  log_opts = {
    "max-size" = "10m"
  }

  # Requires nerdctl >= 2.1.5. Omit to inherit the image healthcheck.
  healthcheck = {
    command      = "curl -f http://localhost/ || exit 1"
    interval     = "30s" # default
    timeout      = "30s" # default
    start_period = "5s"
    retries      = 3 # default
  }

  networks    = [nerdctl_network.app.name] # default bridge when unset
  ip          = "10.5.0.5"                  # static IPv4; needs a known subnet
  mac_address = "02:ac:ce:55:00:01"         # bridge and macvlan networks

  extra_hosts = {
    "db.internal" = "10.5.0.20" # /etc/hosts entries; "host-gateway" works too
  }

  dns        = ["1.1.1.1"] # host resolver config when unset
  dns_opts   = ["ndots:2"]
  dns_search = ["example.internal"]

  env = {
    SOME_VAR = "value"
  }

  ports = [
    { internal = 80, external = 8080 }, # protocol defaults to tcp
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
