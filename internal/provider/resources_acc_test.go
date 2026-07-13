package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccImage_basic(t *testing.T) {
	name := "alpine:3.20"
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "test" {
  name = %q
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone(t, "image", name),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_image.test", "name", name),
					resource.TestCheckResourceAttrSet("nerdctl_image.test", "id"),
					resource.TestCheckResourceAttrSet("nerdctl_image.test", "repo_digest"),
				),
			},
			{
				ResourceName:      "nerdctl_image.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccImage_build stages a build context on the host and builds an image
// from it. Skips when the host has no running buildkitd, which `nerdctl
// build` requires.
func TestAccImage_build(t *testing.T) {
	name := testAccRandomName("tfacc-img") + ":latest"
	dir := "/tmp/" + testAccRandomName("tfacc-build")
	dockerfile := `FROM alpine:3.20
ARG MESSAGE=hello
RUN echo "$MESSAGE" > /message
`
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "test" {
  name = %q

  build = {
    context = %q

    build_args = {
      MESSAGE = "tfacc"
    }
  }
}
`, name, dir)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			ctx := context.Background()
			if err := client.WriteFile(ctx, dir+"/Dockerfile", dockerfile); err != nil {
				t.Fatalf("staging build context: %v", err)
			}
			// Probe for a running buildkitd with a throwaway build of the
			// same context; the test build after it is a cache hit.
			probe := testAccRandomName("tfacc-buildprobe")
			if _, err := client.Run(ctx, "build", "-t", probe, dir); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "buildkit") {
					t.Skipf("no running buildkitd on the test host: %v", err)
				}
				t.Fatalf("probe build: %v", err)
			}
			_, _ = client.Run(ctx, "rmi", probe)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone(t, "image", name),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_image.test", "name", name),
					resource.TestCheckResourceAttrSet("nerdctl_image.test", "id"),
					// containerd assigns the manifest digest at build time,
					// so unlike docker, built images have one before a push.
					resource.TestCheckResourceAttrSet("nerdctl_image.test", "repo_digest"),
				),
			},
		},
	})
}

// TestAccImage_keepLocally destroys an image with keep_locally set and
// asserts it survives on the host.
func TestAccImage_keepLocally(t *testing.T) {
	name := "alpine:3.19"
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "test" {
  name         = %q
  keep_locally = true
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			ctx := context.Background()
			defer func() { _, _ = client.Run(ctx, "rmi", name) }()
			if _, err := client.Run(ctx, "image", "inspect", name); err != nil {
				return fmt.Errorf("image %q should survive destroy with keep_locally: %v", name, err)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("nerdctl_image.test", "id"),
			},
		},
	})
}

func TestAccVolume_basic(t *testing.T) {
	name := testAccRandomName("tfacc-vol")
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_volume" "test" {
  name = %q
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone(t, "volume", name),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_volume.test", "name", name),
					resource.TestCheckResourceAttrSet("nerdctl_volume.test", "mountpoint"),
				),
			},
			{
				ResourceName:                         "nerdctl_volume.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

func TestAccNetwork_basic(t *testing.T) {
	name := testAccRandomName("tfacc-net")
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_network" "test" {
  name    = %q
  subnet  = "10.117.0.0/24"
  gateway = "10.117.0.1"

  labels = {
    "tfacc_label" = "1"
  }
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone(t, "network", name),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("nerdctl_network.test", "subnet", "10.117.0.0/24"),
					resource.TestCheckResourceAttr("nerdctl_network.test", "gateway", "10.117.0.1"),
					resource.TestCheckResourceAttr("nerdctl_network.test", "driver", "bridge"),
					resource.TestCheckResourceAttr("nerdctl_network.test", "labels.tfacc_label", "1"),
					resource.TestCheckResourceAttrSet("nerdctl_network.test", "id"),
				),
			},
			{
				ResourceName:      "nerdctl_network.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			// Out-of-band deletion must plan as a re-create, not an error
			// (missing networks phrase their error unlike other objects).
			{
				PreConfig: func() {
					if _, err := client.Run(context.Background(), "network", "rm", name); err != nil {
						t.Fatalf("out-of-band network rm: %v", err)
					}
				},
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("nerdctl_network.test", "id"),
			},
		},
	})
}
