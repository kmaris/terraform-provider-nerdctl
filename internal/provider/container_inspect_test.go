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
      "RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0},
      "Memory": 536870912,
      "CPUQuota": 150000,
      "CPUPeriod": 100000,
      "Dns": ["1.1.1.1"],
      "DnsOptions": ["ndots:2"],
      "DnsSearch": ["example.internal"]
    },
    "Mounts": [
      {"Type": "bind", "Source": "/var/lib/nerdctl/1935db59/containers/default/1f5a/resolv.conf", "Destination": "/etc/resolv.conf", "Mode": "bind,rprivate", "RW": true, "Propagation": "rprivate"},
      {"Type": "bind", "Source": "/var/lib/nerdctl/1935db59/etchosts/default/1f5a/hosts", "Destination": "/etc/hosts", "Mode": "bind,rprivate", "RW": true, "Propagation": "rprivate"},
      {"Type": "bind", "Source": "/var/lib/nerdctl/1935db59/containers/default/1f5a/hostname", "Destination": "/etc/hostname", "Mode": "bind,rprivate", "RW": true, "Propagation": "rprivate"},
      {"Type": "bind", "Source": "/srv/app", "Destination": "/etc/app", "Mode": "", "RW": false, "Propagation": "rprivate"},
      {"Type": "volume", "Name": "app_config", "Source": "/var/lib/nerdctl/1935db59/volumes/default/app_config/_data", "Destination": "/data", "Mode": "", "RW": true, "Propagation": ""},
      {"Type": "volume", "Name": "9d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f90aabb", "Source": "/var/lib/nerdctl/1935db59/volumes/default/9d1e.../_data", "Destination": "/anon", "Mode": "", "RW": true, "Propagation": ""}
    ],
    "Config": {
      "User": "1000:1000",
      "Hostname": "app-host",
      "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "NGINX_VERSION=1.29.0",
        "FOO=bar",
        "OVERRIDE=user-value",
        "HOSTNAME=1f5a2b3c4d5e"
      ],
      "Labels": {
        "some.label": "value",
        "maintainer": "NGINX Docker Maintainers <docker-maint@nginx.com>",
        "org.opencontainers.image.title": "overridden by user",
        "io.containerd.image.config.stop-signal": "SIGQUIT",
        "containerd.io/restart.policy": "unless-stopped",
        "containerd.io/restart.status": "running",
        "nerdctl/name": "app",
        "nerdctl/namespace": "default",
        "nerdctl/platform": "linux/amd64",
        "nerdctl/state-dir": "/var/lib/nerdctl/1935db59/containers/default/1f5a",
        "nerdctl/anonymous-volumes": "[\"9d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f909d1e2f3a4b5c6d7e8f90aabb\"]",
        "nerdctl/networks": "[\"app-net\",\"other-net\"]",
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
	imageLabels := map[string]string{
		"maintainer":                     "NGINX Docker Maintainers <docker-maint@nginx.com>",
		"org.opencontainers.image.title": "nginx",
	}
	// Image-defined labels and io.containerd.image.config.* are filtered;
	// an image label whose value the user overrode is kept.
	want := map[string]string{
		"some.label":                     "value",
		"org.opencontainers.image.title": "overridden by user",
	}
	if got := info.userLabels(imageLabels); !reflect.DeepEqual(got, want) {
		t.Errorf("userLabels() = %v, want %v", got, want)
	}
}

func TestParseImageInspect(t *testing.T) {
	img, err := parseImageInspect(`[{"Config": {"Labels": {"maintainer": "x"}, "Env": ["NGINX_VERSION=1.29.0"]}}]`)
	if err != nil {
		t.Fatalf("parseImageInspect: %v", err)
	}
	if !reflect.DeepEqual(img.Config.Labels, map[string]string{"maintainer": "x"}) {
		t.Errorf("labels = %v", img.Config.Labels)
	}
	if !reflect.DeepEqual(img.Config.Env, []string{"NGINX_VERSION=1.29.0"}) {
		t.Errorf("env = %v", img.Config.Env)
	}
	if _, err := parseImageInspect("[]"); err == nil {
		t.Error("want error for empty result")
	}
}

func TestInspectUserEnv(t *testing.T) {
	info := mustParseFixture(t)
	imageEnv := []string{
		"NGINX_VERSION=1.29.0",
		"OVERRIDE=image-value",
	}
	// Image entries with unchanged values are subtracted; user overrides are
	// kept. PATH and HOSTNAME stay here — refreshEnv decides on those.
	want := map[string]string{
		"PATH":     "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"FOO":      "bar",
		"OVERRIDE": "user-value",
		"HOSTNAME": "1f5a2b3c4d5e",
	}
	if got := info.userEnv(imageEnv); !reflect.DeepEqual(got, want) {
		t.Errorf("userEnv() = %v, want %v", got, want)
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
	// Sorted by container path; the anonymous volume at /anon and nerdctl's
	// managed resolv.conf/hosts/hostname bind mounts are excluded.
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

func TestInspectNetworks(t *testing.T) {
	info := mustParseFixture(t)
	if got, want := info.networks(), []string{"app-net", "other-net"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networks() = %v, want %v", got, want)
	}

	// Missing label means nil, which refreshNetworks treats as default bridge.
	bare := &containerInspect{}
	if got := bare.networks(); got != nil {
		t.Errorf("networks() = %v, want nil", got)
	}
}

func TestInspectUserHostnameAndLimits(t *testing.T) {
	info := mustParseFixture(t)
	if info.Config.User != "1000:1000" {
		t.Errorf("User = %q, want %q", info.Config.User, "1000:1000")
	}
	if info.Config.Hostname != "app-host" {
		t.Errorf("Hostname = %q, want %q", info.Config.Hostname, "app-host")
	}
	if info.HostConfig.Memory != 536870912 {
		t.Errorf("Memory = %d, want %d", info.HostConfig.Memory, 536870912)
	}
	if got := info.cpus(); got != 1.5 {
		t.Errorf("cpus() = %v, want 1.5", got)
	}

	// Unset quota means unlimited.
	bare := &containerInspect{}
	if got := bare.cpus(); got != 0 {
		t.Errorf("cpus() = %v, want 0", got)
	}
}

func TestParseMemoryBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"512m", 512 << 20, false},
		{"512M", 512 << 20, false},
		{"512mb", 512 << 20, false},
		{"1g", 1 << 30, false},
		{"1.5g", 1610612736, false},
		{"2k", 2048, false},
		{"1024", 1024, false},
		{"100b", 100, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-5m", 0, true},
	}
	for _, tt := range tests {
		got, err := parseMemoryBytes(tt.in)
		if tt.wantErr != (err != nil) {
			t.Errorf("parseMemoryBytes(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("parseMemoryBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestInspectDNS pins the HostConfig JSON key spellings (Dns, not DNS)
// observed in real nerdctl inspect output.
func TestInspectDNS(t *testing.T) {
	info := mustParseFixture(t)
	if want := []string{"1.1.1.1"}; !reflect.DeepEqual(info.HostConfig.DNS, want) {
		t.Errorf("DNS = %v, want %v", info.HostConfig.DNS, want)
	}
	if want := []string{"ndots:2"}; !reflect.DeepEqual(info.HostConfig.DNSOptions, want) {
		t.Errorf("DNSOptions = %v, want %v", info.HostConfig.DNSOptions, want)
	}
	if want := []string{"example.internal"}; !reflect.DeepEqual(info.HostConfig.DNSSearch, want) {
		t.Errorf("DNSSearch = %v, want %v", info.HostConfig.DNSSearch, want)
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
