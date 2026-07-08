package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccContainer_basic(t *testing.T) {
	name := testAccRandomName("tfacc-ctr")
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "test" {
  name  = %q
  image = nerdctl_image.nginx.name
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
					resource.TestCheckResourceAttrSet("nerdctl_container.test", "id"),
					resource.TestCheckResourceAttr("nerdctl_container.test", "restart", "unless-stopped"),
				),
			},
			{
				ResourceName:      "nerdctl_container.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccContainer_full exercises the whole attribute surface at once. The
// framework fails any step whose post-apply plan is not empty, so passing
// proves every refresh helper (including image label/env subtraction and
// the memory/cpus semantic comparison) round-trips real inspect output.
// No import step: entrypoint, user, workdir, hostname, and memory are
// documented as not (or not literally) recoverable.
func TestAccContainer_full(t *testing.T) {
	name := testAccRandomName("tfacc-ctr")
	volName := testAccRandomName("tfacc-vol")
	netName := testAccRandomName("tfacc-net")
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_volume" "data" {
  name = %q
}

resource "nerdctl_network" "net" {
  name   = %q
  subnet = "10.118.0.0/24"
}

resource "nerdctl_container" "test" {
  name    = %q
  image   = nerdctl_image.nginx.name
  restart = "always"

  entrypoint = "/bin/sleep"
  command    = ["infinity"]
  user       = "1000:1000"
  workdir    = "/tmp"
  hostname   = "tfacc-host"
  memory     = "64m"
  cpus       = 0.25

  networks = [nerdctl_network.net.name]

  dns        = ["1.1.1.1", "9.9.9.9"]
  dns_opts   = ["ndots:1"]
  dns_search = ["tfacc.internal"]

  env = {
    TFACC_VAR     = "1"
    NGINX_VERSION = "tfacc-override" # overrides the image env value
  }

  labels = {
    "tfacc_label" = "1"
  }

  ports = [
    { internal = 80, external = 18080 },
    { internal = 69, external = 16969, protocol = "udp" },
  ]

  volumes = [
    { container_path = "/data", volume_name = nerdctl_volume.data.name },
    { container_path = "/hostetc", host_path = "/etc", read_only = true },
  ]
}
`, volName, netName, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testAccComposeGone(
			testAccCheckGone(t, "container", name),
			testAccCheckGone(t, "volume", volName),
			testAccCheckGone(t, "network", netName),
			testAccCheckGone(t, "image", "nginx:alpine"),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("nerdctl_container.test", "id"),
					// Semantic comparison keeps the human-readable forms.
					resource.TestCheckResourceAttr("nerdctl_container.test", "memory", "64m"),
					resource.TestCheckResourceAttr("nerdctl_container.test", "cpus", "0.25"),
					resource.TestCheckResourceAttr("nerdctl_container.test", "dns.0", "1.1.1.1"),
					resource.TestCheckResourceAttr("nerdctl_container.test", "dns.1", "9.9.9.9"),
					resource.TestCheckResourceAttr("nerdctl_container.test", "env.NGINX_VERSION", "tfacc-override"),
				),
			},
		},
	})
}

// TestAccContainer_drift deletes the container out-of-band and expects the
// next step to notice and re-create it cleanly.
func TestAccContainer_drift(t *testing.T) {
	name := testAccRandomName("tfacc-ctr")
	client := testAccClient(t)
	config := testAccProviderConfig() + fmt.Sprintf(`
resource "nerdctl_image" "nginx" {
  name = "nginx:alpine"
}

resource "nerdctl_container" "test" {
  name  = %q
  image = nerdctl_image.nginx.name
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone(t, "container", name),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("nerdctl_container.test", "id"),
			},
			{
				PreConfig: func() {
					if _, err := client.Run(context.Background(), "rm", "-f", name); err != nil {
						t.Fatalf("out-of-band rm: %v", err)
					}
				},
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("nerdctl_container.test", "id"),
			},
		},
	})
}

// TestAccContainer_validators proves the plan-time validators fire before
// anything reaches the host.
func TestAccContainer_validators(t *testing.T) {
	base := testAccProviderConfig() + `
resource "nerdctl_container" "test" {
  name  = "tfacc-invalid"
  image = "nginx:alpine"
%s
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(base, `
  volumes = [
    { container_path = "/data", host_path = "/srv", volume_name = "both" },
  ]`),
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination|exactly one`),
			},
			{
				Config:      fmt.Sprintf(base, `  memory = "12parsecs"`),
				ExpectError: regexp.MustCompile(`must be a size like`),
			},
			{
				Config:      fmt.Sprintf(base, `  restart = "sometimes"`),
				ExpectError: regexp.MustCompile(`must be no, always`),
			},
		},
	})
}
