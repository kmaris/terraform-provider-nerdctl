resource "nerdctl_network" "app" {
  name        = "app-net"
  driver      = "bridge"        # default; also macvlan, ipvlan
  subnet      = "10.5.0.0/24"   # auto-assigned when unset
  gateway     = "10.5.0.1"      # requires subnet
  ip_range    = "10.5.0.128/25" # allocate container IPs from a sub-range
  ipv6_subnet = "fd00:5::/64"   # enables --ipv6

  options = {
    "mtu" = "1450" # driver options passed with -o
  }

  labels = {
    "some.label" = "value"
  }
}
