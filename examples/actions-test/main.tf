terraform {
  required_providers {
    nerdctl = {
      source = "kmaris/nerdctl"
    }
  }
}

# Standalone test config for the provider actions, kept separate from the
# main example so action test runs never disturb that stack. Run it from
# its own state directory:
#
#   mkdir manual.test.actions && cd manual.test.actions
#   ln -s ../examples/actions-test/main.tf main.tf
#   echo 'host = "ssh://your-host.example.com"' > local.auto.tfvars
#   terraform apply
#
# Applying creates one container and fires the actions in its
# action_trigger, in list order. Verify on the host afterwards:
#
#   nerdctl exec tfverify-actions ls -l /tmp/created-by-action
#   ls -lh /tmp/tfverify-images.tar /tmp/tfverify-export.tar
#   nerdctl images | grep tfverify
#
# `terraform destroy` removes the container and image but not the action
# side effects; clean those up on the host manually:
#
#   rm /tmp/tfverify-images.tar /tmp/tfverify-export.tar
#   nerdctl rmi tfverify:imported

variable "host" {
  description = "Remote host to run nerdctl on, as ssh://[user@]host[:port]. Set \"\" to run nerdctl locally."
  type        = string
  default     = "ssh://containers.example.com"
}

provider "nerdctl" {
  host = var.host
}

resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "actions_test" {
  name  = "tfverify-actions"
  image = nerdctl_image.nginx.name

  # Every container attribute forces replacement, so after_create is the
  # only event that ever fires for this resource; the actions run in list
  # order on each (re)create. Re-fire everything with:
  #   terraform apply -replace=nerdctl_container.actions_test
  lifecycle {
    action_trigger {
      events = [after_create]
      actions = [
        action.nerdctl_exec.mark_ready,
        action.nerdctl_image_save.archive,
        action.nerdctl_container_export.backup,
        action.nerdctl_image_load.reload,
        action.nerdctl_image_import.restore,
      ]
    }
  }
}

# Runs inside the container.
action "nerdctl_exec" "mark_ready" {
  config {
    container = nerdctl_container.actions_test.name
    command   = ["sh", "-c", "touch /tmp/created-by-action"]
  }
}

# The tar paths below are written and read on the target host, not the
# machine running Terraform.
action "nerdctl_image_save" "archive" {
  config {
    images = [nerdctl_image.nginx.name]
    output = "/tmp/tfverify-images.tar"
  }
}

action "nerdctl_container_export" "backup" {
  config {
    container = nerdctl_container.actions_test.name
    output    = "/tmp/tfverify-export.tar"
  }
}

# Round-trips the tarball written by image_save above (list order matters).
action "nerdctl_image_load" "reload" {
  config {
    input = "/tmp/tfverify-images.tar"
  }
}

# Imports the rootfs exported by container_export as a new image.
action "nerdctl_image_import" "restore" {
  config {
    input     = "/tmp/tfverify-export.tar"
    reference = "tfverify:imported"
  }
}

# Destructive: sweeps unused objects host-wide, including things Terraform
# does not manage. Uncomment only if that is acceptable on the host.
# action "nerdctl_system_prune" "cleanup" {
#   config {
#     all = true
#   }
# }
