package provider

import (
	"reflect"
	"testing"
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
	if got := networkUserLabels(info.Labels); !reflect.DeepEqual(got, want) {
		t.Errorf("networkUserLabels = %v, want %v", got, want)
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
