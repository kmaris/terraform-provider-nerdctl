package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRegistryImagePushArgs(t *testing.T) {
	m := registryImageResourceModel{Name: types.StringValue("registry.local/app:v1")}
	want := []string{"push", "--quiet", "registry.local/app:v1"}
	if args := registryImagePushArgs(&m); !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}

	m.Platform = types.StringValue("linux/arm64")
	m.InsecureRegistry = types.BoolValue(true)
	want = []string{"push", "--quiet", "--platform", "linux/arm64", "--insecure-registry", "registry.local/app:v1"}
	if args := registryImagePushArgs(&m); !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestParseManifestDigest(t *testing.T) {
	single := `{
  "Ref": "registry.local/app:v1@sha256:aaa",
  "Descriptor": {"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:aaa", "size": 1024}
}`
	got, err := parseManifestDigest(single)
	if err != nil {
		t.Fatalf("single: %v", err)
	}
	if got != "sha256:aaa" {
		t.Errorf("single digest = %q, want sha256:aaa", got)
	}

	oneEntryList := `[{"Ref": "r", "Descriptor": {"digest": "sha256:bbb"}}]`
	got, err = parseManifestDigest(oneEntryList)
	if err != nil {
		t.Fatalf("one-entry list: %v", err)
	}
	if got != "sha256:bbb" {
		t.Errorf("one-entry digest = %q, want sha256:bbb", got)
	}

	// Multi-platform: synthesized, stable across entry order.
	multiA := `[{"Descriptor": {"digest": "sha256:ccc"}}, {"Descriptor": {"digest": "sha256:ddd"}}]`
	multiB := `[{"Descriptor": {"digest": "sha256:ddd"}}, {"Descriptor": {"digest": "sha256:ccc"}}]`
	gotA, err := parseManifestDigest(multiA)
	if err != nil {
		t.Fatalf("multi: %v", err)
	}
	gotB, err := parseManifestDigest(multiB)
	if err != nil {
		t.Fatalf("multi reordered: %v", err)
	}
	if gotA != gotB {
		t.Errorf("synthesized digest not order-stable: %q vs %q", gotA, gotB)
	}
	if gotA == "sha256:ccc" || gotA == "sha256:ddd" {
		t.Errorf("multi digest = %q, want a synthesized value", gotA)
	}

	if _, err := parseManifestDigest("[]"); err == nil {
		t.Error("empty list: expected error")
	}
	if _, err := parseManifestDigest("not json"); err == nil {
		t.Error("garbage: expected error")
	}
}
