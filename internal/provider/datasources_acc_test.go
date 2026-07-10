package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccContainerDataSource creates a running container and reads it back
// through the data source, cross-checking the inspected fields.
func TestAccContainerDataSource(t *testing.T) {
	name := testAccRandomName("tfacc-ctr")
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "test" {
  name  = %q
  image = nerdctl_image.nginx.name

  labels = {
    "tfacc_label" = "1"
  }
}

data "nerdctl_container" "test" {
  name = nerdctl_container.test.name
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testAccComposeGone(
			testAccCheckGone(t, "container", name),
			testAccCheckGone(t, "image", "nginx:alpine"),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.nerdctl_container.test", "id", "nerdctl_container.test", "id"),
					resource.TestCheckResourceAttr("data.nerdctl_container.test", "image", "nginx:alpine"),
					resource.TestCheckResourceAttr("data.nerdctl_container.test", "status", "running"),
					resource.TestCheckResourceAttr("data.nerdctl_container.test", "running", "true"),
					resource.TestCheckResourceAttr("data.nerdctl_container.test", "restart", "unless-stopped"),
					resource.TestCheckResourceAttr("data.nerdctl_container.test", "labels.tfacc_label", "1"),
				),
			},
		},
	})
}

// TestAccDataSources creates one of each object via resources, then reads
// them back through the data sources and cross-checks the attributes.
func TestAccDataSources(t *testing.T) {
	volName := testAccRandomName("tfacc-vol")
	netName := testAccRandomName("tfacc-net")
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "test" {
  name = "alpine:3.20"
}

resource "nerdctl_volume" "test" {
  name = %q
}

resource "nerdctl_network" "test" {
  name   = %q
  subnet = "10.119.0.0/24"

  labels = {
    "tfacc_label" = "1"
  }
}

data "nerdctl_image" "test" {
  name = nerdctl_image.test.name
}

data "nerdctl_volume" "test" {
  name = nerdctl_volume.test.name
}

data "nerdctl_network" "test" {
  name = nerdctl_network.test.name
}
`, volName, netName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testAccComposeGone(
			testAccCheckGone(t, "volume", volName),
			testAccCheckGone(t, "network", netName),
			testAccCheckGone(t, "image", "alpine:3.20"),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.nerdctl_image.test", "id", "nerdctl_image.test", "id"),
					resource.TestCheckResourceAttrPair("data.nerdctl_volume.test", "mountpoint", "nerdctl_volume.test", "mountpoint"),
					resource.TestCheckResourceAttrPair("data.nerdctl_network.test", "id", "nerdctl_network.test", "id"),
					resource.TestCheckResourceAttrPair("data.nerdctl_network.test", "subnet", "nerdctl_network.test", "subnet"),
					resource.TestCheckResourceAttr("data.nerdctl_network.test", "labels.tfacc_label", "1"),
				),
			},
		},
	})
}
