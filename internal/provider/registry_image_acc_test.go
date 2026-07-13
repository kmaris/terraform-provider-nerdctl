package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccRegistryImage_push runs a throwaway registry on the host, pushes a
// tagged image into it, and reads the remote digest back — through the
// resource and the data source, which must agree. Requires nerdctl >= 2.3
// for `nerdctl manifest inspect`.
func TestAccRegistryImage_push(t *testing.T) {
	registryName := testAccRandomName("tfacc-registry")
	const registryPort = "21150"
	ref := fmt.Sprintf("localhost:%s/%s:v1", registryPort, testAccRandomName("tfacc-push"))
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_registry_image" "test" {
  name              = %q
  insecure_registry = true
}

data "nerdctl_registry_image" "test" {
  name              = nerdctl_registry_image.test.name
  insecure_registry = true
}
`, ref)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			ctx := context.Background()
			if _, err := client.Run(ctx, "run", "-d", "--name", registryName,
				"-p", "127.0.0.1:"+registryPort+":5000", "registry:2"); err != nil {
				t.Fatalf("starting throwaway registry: %v", err)
			}
			// The registry binds its port within a moment of starting; there
			// is no cheap readiness probe through the client, so give it a
			// grace period instead.
			time.Sleep(3 * time.Second)
			if _, err := client.Run(ctx, "pull", "--quiet", "alpine:3.20"); err != nil {
				t.Fatalf("pulling source image: %v", err)
			}
			if _, err := client.Run(ctx, "tag", "alpine:3.20", ref); err != nil {
				t.Fatalf("tagging source image: %v", err)
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// Destroy is state-only (nerdctl cannot delete from a registry), so
		// this cleans the host up rather than asserting anything is gone.
		CheckDestroy: func(_ *terraform.State) error {
			ctx := context.Background()
			_, _ = client.Run(ctx, "rm", "-f", registryName)
			_, _ = client.Run(ctx, "rmi", ref)
			_, _ = client.Run(ctx, "rmi", "alpine:3.20")
			_, _ = client.Run(ctx, "rmi", "registry:2")
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_registry_image.test", "id", ref),
					resource.TestMatchResourceAttr("nerdctl_registry_image.test", "sha256_digest",
						regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)),
					// The data source reads the same manifest back from the
					// registry, so the digests must match.
					resource.TestCheckResourceAttrPair(
						"data.nerdctl_registry_image.test", "sha256_digest",
						"nerdctl_registry_image.test", "sha256_digest"),
				),
			},
		},
	})
}
