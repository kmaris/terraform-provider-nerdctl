# Destructive: removes unused objects even if they are not managed by
# Terraform (stopped containers, unused networks and images).
action "nerdctl_system_prune" "cleanup" {
  config {
    all     = true
    volumes = false
  }
}
