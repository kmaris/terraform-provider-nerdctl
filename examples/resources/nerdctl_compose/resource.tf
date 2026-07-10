resource "nerdctl_compose" "app" {
  # project_name is derived from the first file's directory ("app") when omitted
  project_name = "app"
  config_paths = ["/opt/app/compose.yaml"]

  project_directory = "/opt/app"      # relative build contexts and mounts resolve here
  env_files         = ["/opt/app/.env"]
  profiles          = ["prod"]
  remove_orphans    = true

  build          = false # nerdctl extension: pass --build to `compose up`
  remove_volumes = false # nerdctl extension: pass --volumes to `compose down` on destroy
}
