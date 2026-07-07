resource "nerdctl_network" "app" {
  name    = "app-net"
  driver  = "bridge"      # default; also macvlan, ipvlan
  subnet  = "10.5.0.0/24" # auto-assigned when unset
  gateway = "10.5.0.1"    # requires subnet

  labels = {
    "some.label" = "value"
  }
}
