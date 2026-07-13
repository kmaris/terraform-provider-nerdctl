package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Captured from a real `nerdctl network inspect` on a rootless containerd
// host (nerdctl 2.x); the driver is notably absent from the output.
const networkInspectFixture = `[
    {
        "Name": "tfverify-net",
        "Id": "aacd9e3b3e94b659328da2881af2e011247b27ba87802980fefdb3382a53dc36",
        "IPAM": {
            "Config": [
                {
                    "Subnet": "10.99.0.0/24",
                    "Gateway": "10.99.0.1"
                }
            ]
        },
        "Labels": {
            "tfverify.net": "1",
            "nerdctl/default-network": "true"
        },
        "Containers": {}
    }
]`

func TestParseNetworkInspect(t *testing.T) {
	info, err := parseNetworkInspect(networkInspectFixture)
	if err != nil {
		t.Fatalf("parseNetworkInspect: %v", err)
	}
	if want := "aacd9e3b3e94b659328da2881af2e011247b27ba87802980fefdb3382a53dc36"; info.ID != want {
		t.Errorf("ID = %q, want %q", info.ID, want)
	}
	if len(info.IPAM.Config) != 1 {
		t.Fatalf("IPAM.Config length = %d, want 1", len(info.IPAM.Config))
	}
	if got := info.IPAM.Config[0].Subnet; got != "10.99.0.0/24" {
		t.Errorf("Subnet = %q, want %q", got, "10.99.0.0/24")
	}
	if got := info.IPAM.Config[0].Gateway; got != "10.99.0.1" {
		t.Errorf("Gateway = %q, want %q", got, "10.99.0.1")
	}

	want := map[string]string{"tfverify.net": "1"}
	if got := stripNerdctlLabels(info.Labels); !reflect.DeepEqual(got, want) {
		t.Errorf("stripNerdctlLabels = %v, want %v", got, want)
	}
}

func TestParseNetworkInspectErrors(t *testing.T) {
	if _, err := parseNetworkInspect("not json"); err == nil {
		t.Error("want error for invalid JSON")
	}
	if _, err := parseNetworkInspect("[]"); err == nil {
		t.Error("want error for empty result")
	}
}

func TestNetworkCreateArgs(t *testing.T) {
	plan := networkResourceModel{
		Name:       types.StringValue("app-net"),
		Driver:     types.StringValue("bridge"),
		Subnet:     types.StringNull(),
		Gateway:    types.StringNull(),
		IPRange:    types.StringNull(),
		IPv6Subnet: types.StringNull(),
		Options:    types.MapNull(types.StringType),
		Labels:     types.MapNull(types.StringType),
	}

	args, diags := networkCreateArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"network", "create", "--driver", "bridge", "app-net"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}

	plan.Subnet = types.StringValue("10.5.0.0/24")
	plan.Gateway = types.StringValue("10.5.0.1")
	plan.IPRange = types.StringValue("10.5.0.128/25")
	plan.IPv6Subnet = types.StringValue("fd00:5::/64")
	plan.Options = mustMap(t, map[string]string{"mtu": "1450"})
	plan.Labels = mustMap(t, map[string]string{"team": "infra"})

	args, diags = networkCreateArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want = []string{
		"network", "create", "--driver", "bridge",
		"--subnet", "10.5.0.0/24",
		"--gateway", "10.5.0.1",
		"--ip-range", "10.5.0.128/25",
		"--ipv6", "--subnet", "fd00:5::/64",
		"-o", "mtu=1450",
		"--label", "team=infra",
		"app-net",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestNetworkInspectIPFamilies(t *testing.T) {
	ni := &networkInspect{}
	ni.IPAM.Config = []struct {
		Subnet  string `json:"Subnet"`
		Gateway string `json:"Gateway"`
	}{
		{Subnet: "fd00:5::/64"},
		{Subnet: "10.5.0.0/24", Gateway: "10.5.0.1"},
	}

	subnet, gateway, ok := ni.ipv4Config()
	if !ok || subnet != "10.5.0.0/24" || gateway != "10.5.0.1" {
		t.Errorf("ipv4Config = %q/%q/%t, want 10.5.0.0/24, 10.5.0.1, true", subnet, gateway, ok)
	}
	if got := ni.ipv6Subnet(); got != "fd00:5::/64" {
		t.Errorf("ipv6Subnet = %q, want fd00:5::/64", got)
	}
}
