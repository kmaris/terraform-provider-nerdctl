provider "nerdctl" {
  host         = "ssh://user@host.example.com:22"  # omit to run nerdctl locally
  ssh_opts     = ["-i", "~/.ssh/deploy_key"]       # extra ssh options
  sudo         = false                             # run nerdctl under `sudo -n`
  namespace    = "default"                         # containerd namespace
  nerdctl_path = "nerdctl"                         # binary path on the target host
}
