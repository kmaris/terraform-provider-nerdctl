package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// TestAccActions_roundTrip drives exec, image_save, container_export,
// image_load, and image_import through a single after_create trigger, in
// dependency order, then verifies the side effects through the client.
// system_prune is deliberately absent: its blast radius crosses containerd
// namespaces (unused CNI networks), so it stays unit-tested only.
//
// The tarballs written to /tmp on the target host are not cleaned up here:
// nerdctl offers no way to remove host files, and /tmp is transient.
func TestAccActions_roundTrip(t *testing.T) {
	name := testAccRandomName("tfacc-act")
	client := testAccClient(t)
	savedTar := fmt.Sprintf("/tmp/%s-images.tar", name)
	exportTar := fmt.Sprintf("/tmp/%s-export.tar", name)
	importedRef := name + "-imported:v1"

	t.Cleanup(func() {
		// Best-effort: the imported image is created by an action, so
		// destroy does not know about it.
		client.Run(context.Background(), "rmi", importedRef) //nolint:errcheck
	})

	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "test" {
  name  = %[1]q
  image = nerdctl_image.nginx.name

  lifecycle {
    action_trigger {
      events = [after_create]
      actions = [
        action.nerdctl_exec.mark,
        action.nerdctl_image_save.save,
        action.nerdctl_container_export.export,
        action.nerdctl_image_load.load,
        action.nerdctl_image_import.import,
      ]
    }
  }
}

action "nerdctl_exec" "mark" {
  config {
    container = nerdctl_container.test.name
    command   = ["sh", "-c", "touch /tmp/created-by-action"]
  }
}

action "nerdctl_image_save" "save" {
  config {
    images = [nerdctl_image.nginx.name]
    output = %[2]q
  }
}

action "nerdctl_container_export" "export" {
  config {
    container = nerdctl_container.test.name
    output    = %[3]q
  }
}

action "nerdctl_image_load" "load" {
  config {
    input = %[2]q
  }
}

action "nerdctl_image_import" "import" {
  config {
    input     = %[3]q
    reference = %[4]q
  }
}
`, name, savedTar, exportTar, importedRef)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(version.Must(version.NewVersion("1.14.0"))),
		},
		CheckDestroy: testAccComposeGone(
			testAccCheckGone(t, "container", name),
			testAccCheckGone(t, "image", "nginx:alpine"),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("nerdctl_container.test", "id"),
					// exec ran inside the container.
					func(_ *terraform.State) error {
						_, err := client.Run(context.Background(), "exec", name, "ls", "/tmp/created-by-action")
						if err != nil {
							return fmt.Errorf("exec action marker missing: %v", err)
						}
						return nil
					},
					// image_load succeeding proves image_save wrote the
					// tarball; the imported image proves container_export
					// and image_import.
					func(_ *terraform.State) error {
						_, err := client.Run(context.Background(), "image", "inspect", importedRef)
						if err != nil {
							return fmt.Errorf("imported image %s missing: %v", importedRef, err)
						}
						return nil
					},
				),
			},
		},
	})
}

// TestAccActions_containerState drives stop, start, and restart in order
// through one after_create trigger; the container must end up running.
func TestAccActions_containerState(t *testing.T) {
	name := testAccRandomName("tfacc-act")
	client := testAccClient(t)

	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "test" {
  name  = %q
  image = nerdctl_image.nginx.name

  lifecycle {
    action_trigger {
      events = [after_create]
      actions = [
        action.nerdctl_container_stop.stop,
        action.nerdctl_container_start.start,
        action.nerdctl_container_restart.restart,
      ]
    }
  }
}

action "nerdctl_container_stop" "stop" {
  config {
    container = nerdctl_container.test.name
    timeout   = 1
  }
}

action "nerdctl_container_start" "start" {
  config {
    container = nerdctl_container.test.name
  }
}

action "nerdctl_container_restart" "restart" {
  config {
    container = nerdctl_container.test.name
    timeout   = 1
  }
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(version.Must(version.NewVersion("1.14.0"))),
		},
		CheckDestroy: testAccComposeGone(
			testAccCheckGone(t, "container", name),
			testAccCheckGone(t, "image", "nginx:alpine"),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(_ *terraform.State) error {
					out, err := client.Run(context.Background(),
						"inspect", "--format", "{{.State.Running}}", name)
					if err != nil {
						return fmt.Errorf("inspecting container after state actions: %v", err)
					}
					if out != "true" {
						return fmt.Errorf("container not running after stop/start/restart cycle: %q", out)
					}
					return nil
				},
			},
		},
	})
}
