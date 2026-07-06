package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// inspectFixture is shaped like `nerdctl container inspect` dockercompat
// output: raw containerd labels (nerdctl internals included), nat.PortMap
// ports, and an anonymous volume from an image VOLUME directive.
const inspectFixture = `[
  {
    "Id": "1f5a2b3c4d5e6f701f5a2b3c4d5e6f701f5a2b3c4d5e6f701f5a2b3c4d5e6f70",
    "Created": "2026-07-06T10:00:00Z",
    "Path": "/entrypoint.sh",
    "Args": ["--flag=value"],
    "Image": "docker.io/library/traefik:v3",
    "Name": "app",
    "RestartCount": 0,
    "HostConfig": {
      "RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0}
    },
    "Mounts": [
      {"Type": "bind", "Source": "/srv/app", "Destination": "/etc/app", "Mode": "", "RW": false, "Propagation": "rprivate"},
      {"Type": "volume", "Name": "app_config", "Source": "/var/lib/nerdctl/1935db59/volumes/default/app_config/_data", "Destination": "/data", "Mode": "", "RW": true, "Propagation": ""},
      {"Type": "volume", "Name": "9d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f90aabb", "Source": "/var/lib/nerdctl/1935db59/volumes/default/9d1e.../_data", "Destination": "/anon", "Mode": "", "RW": true, "Propagation": ""}
    ],
    "Config": {
      "Labels": {
        "some.label": "value",
        "containerd.io/restart.policy": "unless-stopped",
        "containerd.io/restart.status": "running",
        "nerdctl/name": "app",
        "nerdctl/namespace": "default",
        "nerdctl/platform": "linux/amd64",
        "nerdctl/state-dir": "/var/lib/nerdctl/1935db59/containers/default/1f5a",
        "nerdctl/anonymous-volumes": "[\"9d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f90aabb\"]",
        "nerdctl/ports": "[{\"HostPort\":8080,\"ContainerPort\":80,\"Protocol\":\"tcp\",\"HostIP\":\"0.0.0.0\"},{\"HostPort\":69,\"ContainerPort\":69,\"Protocol\":\"udp\",\"HostIP\":\"0.0.0.0\"}]"
      }
    },
    "NetworkSettings": {
      "Ports": {
        "80/tcp": [{"HostIp": "0.0.0.0", "HostPort": "8080"}],
        "69/udp": [{"HostIp": "0.0.0.0", "HostPort": "69"}],
        "8443/tcp": []
      }
    }
  }
]`

func mustParseFixture(t *testing.T) *containerInspect {
	t.Helper()
	info, err := parseContainerInspect(inspectFixture)
	if err != nil {
		t.Fatalf("parseContainerInspect: %v", err)
	}
	return info
}

func TestParseContainerInspectErrors(t *testing.T) {
	if _, err := parseContainerInspect("not json"); err == nil {
		t.Error("want error for invalid JSON")
	}
	if _, err := parseContainerInspect("[]"); err == nil {
		t.Error("want error for empty result")
	}
}

func TestInspectID(t *testing.T) {
	info := mustParseFixture(t)
	if want := "1f5a2b3c4d5e6f701f5a2b3c4d5e6f701f5a2b3c4d5e6f701f5a2b3c4d5e6f70"; info.ID != want {
		t.Errorf("ID = %q, want %q", info.ID, want)
	}
}

func TestInspectRestartPolicy(t *testing.T) {
	info := mustParseFixture(t)
	if got := info.restartPolicy(); got != "unless-stopped" {
		t.Errorf("restartPolicy() = %q, want %q", got, "unless-stopped")
	}

	// No restart label at all means the policy is "no".
	bare := &containerInspect{}
	if got := bare.restartPolicy(); got != "no" {
		t.Errorf("restartPolicy() = %q, want %q", got, "no")
	}

	// Fall back to HostConfig when the label is missing from Config.Labels.
	fallback := &containerInspect{}
	fallback.HostConfig.RestartPolicy.Name = "on-failure"
	fallback.HostConfig.RestartPolicy.MaximumRetryCount = 3
	if got := fallback.restartPolicy(); got != "on-failure:3" {
		t.Errorf("restartPolicy() = %q, want %q", got, "on-failure:3")
	}
}

func TestInspectUserLabels(t *testing.T) {
	info := mustParseFixture(t)
	want := map[string]string{"some.label": "value"}
	if got := info.userLabels(); !reflect.DeepEqual(got, want) {
		t.Errorf("userLabels() = %v, want %v", got, want)
	}
}

func TestInspectPortModels(t *testing.T) {
	info := mustParseFixture(t)
	got, err := info.portModels()
	if err != nil {
		t.Fatalf("portModels: %v", err)
	}
	// Sorted by internal port; the exposed-but-unpublished 8443/tcp is skipped.
	want := []portModel{
		{Internal: types.Int64Value(69), External: types.Int64Value(69), Protocol: types.StringValue("udp")},
		{Internal: types.Int64Value(80), External: types.Int64Value(8080), Protocol: types.StringValue("tcp")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("portModels() = %v, want %v", got, want)
	}
}

func TestInspectVolumeMounts(t *testing.T) {
	info := mustParseFixture(t)
	got := info.volumeMounts()
	// Sorted by container path; the anonymous volume at /anon is excluded.
	want := []volumeMountModel{
		{
			ContainerPath: types.StringValue("/data"),
			HostPath:      types.StringNull(),
			VolumeName:    types.StringValue("app_config"),
			ReadOnly:      types.BoolValue(false),
		},
		{
			ContainerPath: types.StringValue("/etc/app"),
			HostPath:      types.StringValue("/srv/app"),
			VolumeName:    types.StringNull(),
			ReadOnly:      types.BoolValue(true),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("volumeMounts() = %v, want %v", got, want)
	}
}

func TestNormalizeImageRef(t *testing.T) {
	tests := []struct{ in, want string }{
		{"traefik:v3", "traefik:v3"},
		{"docker.io/library/traefik:v3", "traefik:v3"},
		{"docker.io/netbootxyz/netbootxyz", "netbootxyz/netbootxyz"},
		{"ghcr.io/netbootxyz/netbootxyz", "ghcr.io/netbootxyz/netbootxyz"},
	}
	for _, tt := range tests {
		if got := normalizeImageRef(tt.in); got != tt.want {
			t.Errorf("normalizeImageRef(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPortSetsEqual(t *testing.T) {
	a := []portModel{
		{Internal: types.Int64Value(80), External: types.Int64Value(8080), Protocol: types.StringValue("tcp")},
		{Internal: types.Int64Value(69), External: types.Int64Value(69), Protocol: types.StringValue("udp")},
	}
	reordered := []portModel{a[1], a[0]}
	if !portSetsEqual(a, reordered) {
		t.Error("portSetsEqual is order sensitive, want order insensitive")
	}
	changed := []portModel{a[0], {Internal: types.Int64Value(69), External: types.Int64Value(70), Protocol: types.StringValue("udp")}}
	if portSetsEqual(a, changed) {
		t.Error("portSetsEqual missed a changed external port")
	}
	if portSetsEqual(a, a[:1]) {
		t.Error("portSetsEqual missed a length difference")
	}
	if !portSetsEqual(nil, nil) {
		t.Error("portSetsEqual(nil, nil) = false, want true")
	}
}

func TestMountSetsEqual(t *testing.T) {
	a := []volumeMountModel{
		{ContainerPath: types.StringValue("/data"), HostPath: types.StringNull(), VolumeName: types.StringValue("cfg"), ReadOnly: types.BoolValue(false)},
		{ContainerPath: types.StringValue("/etc/app"), HostPath: types.StringValue("/srv/app"), VolumeName: types.StringNull(), ReadOnly: types.BoolValue(true)},
	}
	reordered := []volumeMountModel{a[1], a[0]}
	if !mountSetsEqual(a, reordered) {
		t.Error("mountSetsEqual is order sensitive, want order insensitive")
	}
	changed := make([]volumeMountModel, len(a))
	copy(changed, a)
	changed[1].ReadOnly = types.BoolValue(false)
	if mountSetsEqual(a, changed) {
		t.Error("mountSetsEqual missed a read_only change")
	}
}
