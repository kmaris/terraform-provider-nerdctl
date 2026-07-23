package provider

// Acceptance tests: real terraform plan/apply/destroy cycles against a real
// containerd host, gated behind TF_ACC per terraform-plugin-testing
// convention. The target host comes from NERDCTL_TEST_HOST (empty runs
// nerdctl locally) and objects are created in a dedicated containerd
// namespace (NERDCTL_TEST_NAMESPACE, default "tfacc") so test objects never
// mix with real workloads. Run with:
//
//	NERDCTL_TEST_HOST=ssh://host TF_ACC=1 go test -v -run TestAcc ./internal/provider/ -timeout 30m
//
// Note: host port bindings and CNI networks are host-global despite the
// namespace isolation, so tests use high ports and random names.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"nerdctl": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccHost() string { return os.Getenv("NERDCTL_TEST_HOST") }
func testAccNamespace() string {
	if ns := os.Getenv("NERDCTL_TEST_NAMESPACE"); ns != "" {
		return ns
	}
	return "tfacc"
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if testAccHost() == "" {
		if _, err := exec.LookPath("nerdctl"); err != nil {
			t.Fatal("acceptance tests need NERDCTL_TEST_HOST (ssh://...) or a local nerdctl binary in PATH")
		}
	}
}

// testAccProviderConfig is prepended to every test configuration.
func testAccProviderConfig() string {
	return fmt.Sprintf(`
provider "nerdctl" {
  host      = %q
  namespace = %q
}
`, testAccHost(), testAccNamespace())
}

// testAccClient talks to the same host and namespace as the provider under
// test, for out-of-band setup and verification.
func testAccClient(t *testing.T) *nerdctl.Client {
	t.Helper()
	client, err := nerdctl.New(nerdctl.Config{
		Host:      testAccHost(),
		Namespace: testAccNamespace(),
	})
	if err != nil {
		t.Fatalf("building test client: %v", err)
	}
	return client
}

func testAccRandomName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, acctest.RandString(8))
}

// testAccCheckGone returns a CheckDestroy-compatible func asserting the
// object no longer exists on the host. kind is the nerdctl inspect noun:
// "container", "image", "volume", or "network".
func testAccCheckGone(t *testing.T, kind, name string) func(*terraform.State) error {
	t.Helper()
	client := testAccClient(t)
	return func(_ *terraform.State) error {
		_, err := client.Run(context.Background(), kind, "inspect", name)
		if err == nil {
			return fmt.Errorf("%s %q still exists after destroy", kind, name)
		}
		if !nerdctl.NotFound(err) {
			return fmt.Errorf("inspecting %s %q: %v", kind, name, err)
		}
		return nil
	}
}

// testAccComposeGone combines several gone-checks into one CheckDestroy.
func testAccComposeGone(checks ...func(*terraform.State) error) func(*terraform.State) error {
	return func(s *terraform.State) error {
		for _, check := range checks {
			if err := check(s); err != nil {
				return err
			}
		}
		return nil
	}
}

// testAccSkipBelowNerdctl skips the test when nerdctl is older than min (ie 2.3.0)
// Version-gated features call this in PreCheck
func testAccSkipBelowNerdctl(t *testing.T, minVer string) {
	t.Helper()
	client := testAccClient(t)
	out, err := client.Run(context.Background(), "version", "--format", "{{.Client.Version}}")
	if err != nil {
		t.Fatalf("querying nerdctl version: %v", err)
	}
	haveVer, err := version.NewVersion(out)
	if err != nil {
		t.Fatalf("parsing nerdctl version %q: %v", out, err)
	}
	if haveVer.LessThan(version.Must(version.NewVersion(minVer))) {
		t.Skipf("test needs nerdctl >= %s, host has %s", minVer, out)
	}
}
