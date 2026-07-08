resource "nerdctl_container" "app" {
  name  = "app"
  image = "nginx:alpine"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.nerdctl_exec.mark_ready]
    }
  }
}

action "nerdctl_exec" "mark_ready" {
  config {
    container = nerdctl_container.app.name
    command   = ["sh", "-c", "echo ready > /usr/share/nginx/html/status"]
  }
}
