package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
