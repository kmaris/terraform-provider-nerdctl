package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

// TestAccCompose stages a one-service compose file on the host, brings the
// project up, and checks the reported services. The compose file is written
// through the client so this works both locally and over ssh.
func TestAccCompose(t *testing.T) {
	name := testAccRandomName("tfacc-compose")
	dir := "/tmp/" + testAccRandomName("tfacc-comp")
	composeFile := dir + "/compose.yaml"
	composeYAML := `services:
  web:
    image: nginx:alpine
`
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_compose" "test" {
  project_name = %q
  config_paths = [%q]
}
`, name, composeFile)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if err := client.WriteFile(context.Background(), composeFile, composeYAML); err != nil {
				t.Fatalf("staging compose file: %v", err)
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckComposeGone(t, name, composeFile),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_compose.test", "id", name),
					resource.TestCheckResourceAttr("nerdctl_compose.test", "services.#", "1"),
					resource.TestCheckResourceAttr("nerdctl_compose.test", "services.0", "web"),
				),
			},
		},
	})
}

// testAccCheckComposeGone asserts the compose project has no running
// containers left after destroy.
func testAccCheckComposeGone(t *testing.T, project, composeFile string) func(*terraform.State) error {
	t.Helper()
	client := testAccClient(t)
	return func(_ *terraform.State) error {
		out, err := client.Run(context.Background(), "compose", "-p", project, "-f", composeFile, "ps", "-q")
		if err != nil {
			if nerdctl.NotFound(err) {
				return nil
			}
			return fmt.Errorf("checking compose project %q: %v", project, err)
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("compose project %q still has containers: %s", project, out)
		}
		return nil
	}
}
