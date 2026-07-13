package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// minimalContainerModel mirrors a plan after defaults: restart, privileged,
// and log_driver are always set, everything optional is null.
func minimalContainerModel() containerResourceModel {
	return containerResourceModel{
		Name:          types.StringValue("app"),
		Image:         types.StringValue("traefik:v3"),
		Restart:       types.StringValue("unless-stopped"),
		Privileged:    types.BoolValue(false),
		ReadOnly:      types.BoolValue(false),
		SecurityOpt:   types.ListNull(types.StringType),
		GroupAdd:      types.ListNull(types.StringType),
		Devices:       types.ListNull(deviceObjectType),
		Ulimits:       types.ListNull(ulimitObjectType),
		ShmSize:       types.StringNull(),
		Pid:           types.StringNull(),
		Ipc:           types.StringNull(),
		Init:          types.BoolNull(),
		StopSignal:    types.StringNull(),
		StopTimeout:   types.Int64Null(),
		Platform:      types.StringNull(),
		LogDriver:     types.StringValue("json-file"),
		Command:       types.ListNull(types.StringType),
		CapAdd:        types.ListNull(types.StringType),
		CapDrop:       types.ListNull(types.StringType),
		Sysctls:       types.MapNull(types.StringType),
		Tmpfs:         types.MapNull(types.StringType),
		LogOpts:       types.MapNull(types.StringType),
		Healthcheck:   types.ObjectNull(healthcheckObjectType.AttrTypes),
		NoHealthcheck: types.BoolNull(),
		Wait:          types.BoolNull(),
		WaitTimeout:   types.Int64Null(),
		Networks:      types.ListNull(types.StringType),
		IP:            types.StringNull(),
		IP6:           types.StringNull(),
		MacAddress:    types.StringNull(),
		ExtraHosts:    types.MapNull(types.StringType),
		DNS:           types.ListNull(types.StringType),
		DNSOpts:       types.ListNull(types.StringType),
		DNSSearch:     types.ListNull(types.StringType),
		Env:           types.MapNull(types.StringType),
		Ports:         types.ListNull(portObjectType),
		Labels:        types.MapNull(types.StringType),
		Volumes:       types.ListNull(volumeObjectType),
	}
}

func mustMap(t *testing.T, elems map[string]string) types.Map {
	t.Helper()
	m, diags := types.MapValueFrom(context.Background(), types.StringType, elems)
	if diags.HasError() {
		t.Fatalf("building map: %v", diags)
	}
	return m
}

func mustHealthcheck(t *testing.T, hc healthcheckModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(context.Background(), healthcheckObjectType.AttrTypes, hc)
	if diags.HasError() {
		t.Fatalf("building healthcheck: %v", diags)
	}
	return obj
}

func mustList(t *testing.T, elemType attr.Type, elems any) types.List {
	t.Helper()
	l, diags := types.ListValueFrom(context.Background(), elemType, elems)
	if diags.HasError() {
		t.Fatalf("building list: %v", diags)
	}
	return l
}

func TestBuildRunArgsMinimal(t *testing.T) {
	plan := minimalContainerModel()

	args, diags := buildRunArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"run", "-d", "--name", "app", "--restart", "unless-stopped", "--log-driver", "json-file", "traefik:v3"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestBuildRunArgsFull(t *testing.T) {
	plan := minimalContainerModel()
	plan.Command = mustList(t, types.StringType, []string{"--flag=value", "serve"})
	plan.Entrypoint = types.StringValue("/bin/app")
	plan.User = types.StringValue("1000:1000")
	plan.Workdir = types.StringValue("/srv")
	plan.Hostname = types.StringValue("app-host")
	plan.Memory = types.StringValue("512m")
	plan.Cpus = types.Float64Value(1.5)
	plan.Privileged = types.BoolValue(true)
	plan.CapAdd = mustList(t, types.StringType, []string{"NET_ADMIN", "SYS_TIME"})
	plan.CapDrop = mustList(t, types.StringType, []string{"MKNOD"})
	plan.ReadOnly = types.BoolValue(true)
	plan.SecurityOpt = mustList(t, types.StringType, []string{"no-new-privileges"})
	plan.GroupAdd = mustList(t, types.StringType, []string{"video", "1000"})
	plan.Devices = mustList(t, deviceObjectType, []deviceModel{
		{
			HostPath:      types.StringValue("/dev/fuse"),
			ContainerPath: types.StringNull(),
			Permissions:   types.StringValue("rwm"),
		},
		{
			HostPath:      types.StringValue("/dev/null"),
			ContainerPath: types.StringValue("/dev/testnull"),
			Permissions:   types.StringValue("rw"),
		},
	})
	plan.Ulimits = mustList(t, ulimitObjectType, []ulimitModel{
		{Name: types.StringValue("nofile"), Soft: types.Int64Value(1024), Hard: types.Int64Value(2048)},
	})
	plan.ShmSize = types.StringValue("128m")
	plan.Pid = types.StringValue("host")
	plan.Ipc = types.StringValue("private")
	plan.Init = types.BoolValue(true)
	plan.StopSignal = types.StringValue("SIGQUIT")
	plan.StopTimeout = types.Int64Value(5)
	plan.Platform = types.StringValue("linux/amd64")
	plan.Sysctls = mustMap(t, map[string]string{"net.core.somaxconn": "512"})
	plan.Tmpfs = mustMap(t, map[string]string{"/scratch": "", "/run": "size=64m"})
	plan.LogDriver = types.StringValue("journald")
	plan.LogOpts = mustMap(t, map[string]string{"tag": "app"})
	plan.Healthcheck = mustHealthcheck(t, healthcheckModel{
		Command:     types.StringValue("curl -f http://localhost/ || exit 1"),
		Interval:    types.StringValue("10s"),
		Timeout:     types.StringValue("5s"),
		StartPeriod: types.StringValue("2s"),
		Retries:     types.Int64Value(2),
	})
	plan.Networks = mustList(t, types.StringType, []string{"app-net", "other-net"})
	plan.IP = types.StringValue("10.4.0.5")
	plan.IP6 = types.StringValue("fd00::5")
	plan.MacAddress = types.StringValue("02:ac:ce:55:00:01")
	plan.ExtraHosts = mustMap(t, map[string]string{
		"gw.internal": "host-gateway",
		"db.internal": "10.4.0.20",
	})
	plan.DNS = mustList(t, types.StringType, []string{"8.8.8.8", "1.1.1.1"})
	plan.DNSOpts = mustList(t, types.StringType, []string{"ndots:2"})
	plan.DNSSearch = mustList(t, types.StringType, []string{"example.internal"})
	plan.Ports = mustList(t, portObjectType, []portModel{
		{Internal: types.Int64Value(80), External: types.Int64Value(8080), Protocol: types.StringValue("tcp")},
		{Internal: types.Int64Value(69), External: types.Int64Value(69), Protocol: types.StringValue("udp")},
	})
	labels, diags := types.MapValueFrom(context.Background(), types.StringType, map[string]string{
		"b.label": "2",
		"a.label": "1",
	})
	if diags.HasError() {
		t.Fatalf("building labels: %v", diags)
	}
	plan.Labels = labels
	env, diags := types.MapValueFrom(context.Background(), types.StringType, map[string]string{
		"B_VAR": "2",
		"A_VAR": "1",
	})
	if diags.HasError() {
		t.Fatalf("building env: %v", diags)
	}
	plan.Env = env
	plan.Volumes = mustList(t, volumeObjectType, []volumeMountModel{
		{
			ContainerPath: types.StringValue("/etc/app"),
			HostPath:      types.StringValue("/srv/app"),
			VolumeName:    types.StringNull(),
			ReadOnly:      types.BoolValue(true),
		},
		{
			ContainerPath: types.StringValue("/data"),
			HostPath:      types.StringNull(),
			VolumeName:    types.StringValue("app_config"),
			ReadOnly:      types.BoolValue(false),
		},
	})

	args, d := buildRunArgs(context.Background(), &plan)
	if d.HasError() {
		t.Fatalf("unexpected diagnostics: %v", d)
	}
	want := []string{
		"run", "-d", "--name", "app",
		"--restart", "unless-stopped",
		"--entrypoint", "/bin/app",
		"--user", "1000:1000",
		"--workdir", "/srv",
		"--hostname", "app-host",
		"--memory", "512m",
		"--cpus", "1.5",
		"--privileged",
		"--cap-add", "NET_ADMIN",
		"--cap-add", "SYS_TIME",
		"--cap-drop", "MKNOD",
		"--read-only",
		"--security-opt", "no-new-privileges",
		"--group-add", "video",
		"--group-add", "1000",
		"--device", "/dev/fuse:/dev/fuse:rwm", // null container_path defaults to host_path
		"--device", "/dev/null:/dev/testnull:rw",
		"--ulimit", "nofile=1024:2048",
		"--shm-size", "128m",
		"--pid", "host",
		"--ipc", "private",
		"--init",
		"--stop-signal", "SIGQUIT",
		"--stop-timeout", "5",
		"--platform", "linux/amd64",
		"--sysctl", "net.core.somaxconn=512",
		"--tmpfs", "/run:size=64m", // map keys sorted; empty options omit the colon
		"--tmpfs", "/scratch",
		"--log-driver", "journald",
		"--log-opt", "tag=app",
		"--health-cmd", "curl -f http://localhost/ || exit 1",
		"--health-interval", "10s",
		"--health-timeout", "5s",
		"--health-retries", "2",
		"--health-start-period", "2s",
		"--net", "app-net",
		"--net", "other-net",
		"--ip", "10.4.0.5",
		"--ip6", "fd00::5",
		"--mac-address", "02:ac:ce:55:00:01",
		"--add-host", "db.internal:10.4.0.20", // map keys sorted
		"--add-host", "gw.internal:host-gateway",
		"--dns", "8.8.8.8",
		"--dns", "1.1.1.1",
		"--dns-option", "ndots:2",
		"--dns-search", "example.internal",
		"-p", "8080:80/tcp",
		"-p", "69:69/udp",
		"--label", "a.label=1", // map keys must come out sorted, not in map order
		"--label", "b.label=2",
		"-e", "A_VAR=1",
		"-e", "B_VAR=2",
		"-v", "/srv/app:/etc/app:ro",
		"-v", "app_config:/data",
		"traefik:v3",
		"--flag=value", "serve",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestRefreshEnvIgnoresRuntimeDefaults(t *testing.T) {
	// A container with no user env still carries the containerd default
	// PATH and an injected HOSTNAME in its spec; state must stay null.
	info := &containerInspect{}
	info.Config.Env = []string{"PATH=" + defaultSpecPathValue, "HOSTNAME=abc123"}
	state := minimalContainerModel()

	if diags := refreshEnv(context.Background(), &state, info, nil); diags.HasError() {
		t.Fatalf("refreshEnv: %v", diags)
	}
	if !state.Env.IsNull() {
		t.Errorf("state.Env = %v, want null", state.Env)
	}
}

func TestRefreshEnvKeepsManagedRuntimeKeys(t *testing.T) {
	// When the user manages PATH (even at the default value), it is kept
	// and compares equal — no dirtying, no dropping.
	info := &containerInspect{}
	info.Config.Env = []string{"PATH=" + defaultSpecPathValue}
	state := minimalContainerModel()
	env, diags := types.MapValueFrom(context.Background(), types.StringType, map[string]string{"PATH": defaultSpecPathValue})
	if diags.HasError() {
		t.Fatalf("building env: %v", diags)
	}
	state.Env = env

	if diags := refreshEnv(context.Background(), &state, info, nil); diags.HasError() {
		t.Fatalf("refreshEnv: %v", diags)
	}
	got := map[string]string{}
	if diags := state.Env.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading env: %v", diags)
	}
	if want := map[string]string{"PATH": defaultSpecPathValue}; !reflect.DeepEqual(got, want) {
		t.Errorf("state.Env = %v, want %v", got, want)
	}
}

func TestRefreshEnvDetectsDrift(t *testing.T) {
	info := &containerInspect{}
	info.Config.Env = []string{"FOO=bar"}
	state := minimalContainerModel()

	if diags := refreshEnv(context.Background(), &state, info, nil); diags.HasError() {
		t.Fatalf("refreshEnv: %v", diags)
	}
	got := map[string]string{}
	if diags := state.Env.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading env: %v", diags)
	}
	if want := map[string]string{"FOO": "bar"}; !reflect.DeepEqual(got, want) {
		t.Errorf("state.Env = %v, want %v", got, want)
	}
}

func TestRefreshManagedString(t *testing.T) {
	// Unmanaged: image/runtime defaults are ignored.
	s := types.StringNull()
	refreshManagedString(&s, "nginx")
	if !s.IsNull() {
		t.Errorf("unmanaged attribute updated to %v, want null", s)
	}

	// Managed and matching: untouched.
	s = types.StringValue("1000")
	refreshManagedString(&s, "1000")
	if s.ValueString() != "1000" {
		t.Errorf("matching value changed to %v", s)
	}

	// Managed and drifted: updated.
	refreshManagedString(&s, "2000")
	if s.ValueString() != "2000" {
		t.Errorf("drifted value = %v, want 2000", s)
	}

	// Managed but actual empty: nulled.
	refreshManagedString(&s, "")
	if !s.IsNull() {
		t.Errorf("value = %v, want null", s)
	}
}

func TestRefreshMemoryAndCpus(t *testing.T) {
	info := &containerInspect{}
	info.HostConfig.Memory = 512 << 20
	info.HostConfig.CPUQuota = 150000
	info.HostConfig.CPUPeriod = 100000

	// Managed and semantically equal: state keeps its human-readable form.
	state := minimalContainerModel()
	state.Memory = types.StringValue("512m")
	state.Cpus = types.Float64Value(1.5)
	if diags := refreshMemory(&state, info); diags.HasError() {
		t.Fatalf("refreshMemory: %v", diags)
	}
	refreshCpus(&state, info)
	if state.Memory.ValueString() != "512m" {
		t.Errorf("Memory = %v, want 512m kept", state.Memory)
	}
	if state.Cpus.ValueFloat64() != 1.5 {
		t.Errorf("Cpus = %v, want 1.5 kept", state.Cpus)
	}

	// Unmanaged with limits set out-of-band: real drift, reported.
	state = minimalContainerModel()
	if diags := refreshMemory(&state, info); diags.HasError() {
		t.Fatalf("refreshMemory: %v", diags)
	}
	refreshCpus(&state, info)
	if state.Memory.ValueString() != "536870912" {
		t.Errorf("Memory = %v, want 536870912", state.Memory)
	}
	if state.Cpus.ValueFloat64() != 1.5 {
		t.Errorf("Cpus = %v, want 1.5", state.Cpus)
	}

	// Managed but limits removed out-of-band: nulled.
	unlimited := &containerInspect{}
	state = minimalContainerModel()
	state.Memory = types.StringValue("512m")
	state.Cpus = types.Float64Value(1.5)
	if diags := refreshMemory(&state, unlimited); diags.HasError() {
		t.Fatalf("refreshMemory: %v", diags)
	}
	refreshCpus(&state, unlimited)
	if !state.Memory.IsNull() {
		t.Errorf("Memory = %v, want null", state.Memory)
	}
	if !state.Cpus.IsNull() {
		t.Errorf("Cpus = %v, want null", state.Cpus)
	}
}

func TestRefreshStringList(t *testing.T) {
	ctx := context.Background()

	// Null state and empty actual are equivalent: no dirtying.
	l := types.ListNull(types.StringType)
	if diags := refreshStringList(ctx, &l, nil); diags.HasError() {
		t.Fatalf("refreshStringList: %v", diags)
	}
	if !l.IsNull() {
		t.Errorf("list = %v, want null", l)
	}

	// Drift onto a null state is written (the import path).
	if diags := refreshStringList(ctx, &l, []string{"1.1.1.1", "8.8.8.8"}); diags.HasError() {
		t.Fatalf("refreshStringList: %v", diags)
	}
	var got []string
	if diags := l.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading list: %v", diags)
	}
	if want := []string{"1.1.1.1", "8.8.8.8"}; !reflect.DeepEqual(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	// Equal values leave state untouched.
	if diags := refreshStringList(ctx, &l, []string{"1.1.1.1", "8.8.8.8"}); diags.HasError() {
		t.Fatalf("refreshStringList: %v", diags)
	}
	if l.IsNull() {
		t.Error("equal refresh nulled the list")
	}

	// Reordered values are drift: the comparison is ordered.
	if diags := refreshStringList(ctx, &l, []string{"8.8.8.8", "1.1.1.1"}); diags.HasError() {
		t.Fatalf("refreshStringList: %v", diags)
	}
	got = nil
	if diags := l.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading list: %v", diags)
	}
	if want := []string{"8.8.8.8", "1.1.1.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}

	// Values removed out-of-band null the state.
	if diags := refreshStringList(ctx, &l, nil); diags.HasError() {
		t.Fatalf("refreshStringList: %v", diags)
	}
	if !l.IsNull() {
		t.Errorf("list = %v, want null", l)
	}
}

func TestRefreshNetworksBridgeDefault(t *testing.T) {
	// A null state on the default bridge never shows drift...
	info := &containerInspect{}
	info.Config.Labels = map[string]string{"nerdctl/networks": `["bridge"]`}
	state := minimalContainerModel()
	if diags := refreshNetworks(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshNetworks: %v", diags)
	}
	if !state.Networks.IsNull() {
		t.Errorf("Networks = %v, want null", state.Networks)
	}

	// ...but a managed network list still tracks a move back to bridge.
	state.Networks = mustList(t, types.StringType, []string{"app-net"})
	if diags := refreshNetworks(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshNetworks: %v", diags)
	}
	var got []string
	if diags := state.Networks.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading networks: %v", diags)
	}
	if want := []string{"bridge"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Networks = %v, want %v", got, want)
	}
}

func TestRefreshNetworkIdentity(t *testing.T) {
	// Labels hold the configured --ip/--ip6/--mac-address verbatim, so the
	// refresh fills unmanaged values (import) and nulls removed ones.
	info := &containerInspect{}
	info.Config.Labels = map[string]string{
		"nerdctl/ip":          "10.4.0.5",
		"nerdctl/mac-address": "02:ac:ce:55:00:01",
	}
	state := minimalContainerModel()

	refreshLabelString(&state.IP, info.staticIP())
	refreshLabelString(&state.IP6, info.staticIP6())
	refreshLabelString(&state.MacAddress, info.macAddress())

	if got := state.IP.ValueString(); got != "10.4.0.5" {
		t.Errorf("IP = %q, want 10.4.0.5", got)
	}
	if !state.IP6.IsNull() {
		t.Errorf("IP6 = %v, want null", state.IP6)
	}
	if got := state.MacAddress.ValueString(); got != "02:ac:ce:55:00:01" {
		t.Errorf("MacAddress = %q, want 02:ac:ce:55:00:01", got)
	}

	// A container recreated out-of-band without the flags nulls them.
	refreshLabelString(&state.IP, "")
	if !state.IP.IsNull() {
		t.Errorf("IP = %v, want null after label removal", state.IP)
	}
}

func TestRefreshExtraHosts(t *testing.T) {
	info := &containerInspect{}
	info.Config.Labels = map[string]string{
		"nerdctl/extraHosts": `["db.internal:10.4.0.20","v6.internal:fd00::20"]`,
	}
	state := minimalContainerModel()

	if diags := refreshExtraHosts(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshExtraHosts: %v", diags)
	}
	got := map[string]string{}
	if diags := state.ExtraHosts.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading extra_hosts: %v", diags)
	}
	// IPv6 values survive the first-colon cut.
	want := map[string]string{"db.internal": "10.4.0.20", "v6.internal": "fd00::20"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtraHosts = %v, want %v", got, want)
	}

	// Matching entries leave state untouched; a missing label nulls it.
	info.Config.Labels = map[string]string{}
	if diags := refreshExtraHosts(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshExtraHosts: %v", diags)
	}
	if !state.ExtraHosts.IsNull() {
		t.Errorf("ExtraHosts = %v, want null", state.ExtraHosts)
	}
}

func TestRefreshDevices(t *testing.T) {
	// Inspect reports only user-passed devices, so the refresh fills
	// unmanaged state (import) and compares null container_path as the
	// host path without dirtying state.
	info := &containerInspect{}
	info.HostConfig.Devices = []struct {
		PathOnHost        string `json:"PathOnHost"`
		PathInContainer   string `json:"PathInContainer"`
		CgroupPermissions string `json:"CgroupPermissions"`
	}{{PathOnHost: "/dev/fuse", PathInContainer: "/dev/fuse", CgroupPermissions: "rwm"}}

	state := minimalContainerModel()
	state.Devices = mustList(t, deviceObjectType, []deviceModel{{
		HostPath:      types.StringValue("/dev/fuse"),
		ContainerPath: types.StringNull(),
		Permissions:   types.StringValue("rwm"),
	}})
	before := state.Devices
	if diags := refreshDevices(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshDevices: %v", diags)
	}
	if !state.Devices.Equal(before) {
		t.Errorf("Devices dirtied on semantic match: %v", state.Devices)
	}

	// Out-of-band removal nulls the list.
	info.HostConfig.Devices = nil
	if diags := refreshDevices(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshDevices: %v", diags)
	}
	if !state.Devices.IsNull() {
		t.Errorf("Devices = %v, want null", state.Devices)
	}
}

func TestRefreshUlimitsManagedOnly(t *testing.T) {
	// Runtime-default ulimits in inspect output must not surface on an
	// unmanaged attribute...
	info := &containerInspect{}
	info.HostConfig.Ulimits = []struct {
		Name string `json:"Name"`
		Soft int64  `json:"Soft"`
		Hard int64  `json:"Hard"`
	}{{Name: "nofile", Soft: 1024, Hard: 4096}}

	state := minimalContainerModel()
	if diags := refreshUlimits(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshUlimits: %v", diags)
	}
	if !state.Ulimits.IsNull() {
		t.Errorf("Ulimits = %v, want null", state.Ulimits)
	}

	// ...but a managed list tracks real drift.
	state.Ulimits = mustList(t, ulimitObjectType, []ulimitModel{
		{Name: types.StringValue("nofile"), Soft: types.Int64Value(1024), Hard: types.Int64Value(2048)},
	})
	if diags := refreshUlimits(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshUlimits: %v", diags)
	}
	var got []ulimitModel
	if diags := state.Ulimits.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading ulimits: %v", diags)
	}
	if len(got) != 1 || got[0].Hard.ValueInt64() != 4096 {
		t.Errorf("Ulimits = %v, want hard=4096", got)
	}
}

func TestRefreshShmSizeManagedOnly(t *testing.T) {
	info := &containerInspect{}
	info.HostConfig.ShmSize = 64 << 20

	// Unmanaged stays null even though inspect reports the runtime default.
	state := minimalContainerModel()
	if diags := refreshShmSize(&state, info); diags.HasError() {
		t.Fatalf("refreshShmSize: %v", diags)
	}
	if !state.ShmSize.IsNull() {
		t.Errorf("ShmSize = %v, want null", state.ShmSize)
	}

	// A managed value compares semantically and keeps its spelling.
	state.ShmSize = types.StringValue("64m")
	if diags := refreshShmSize(&state, info); diags.HasError() {
		t.Fatalf("refreshShmSize: %v", diags)
	}
	if got := state.ShmSize.ValueString(); got != "64m" {
		t.Errorf("ShmSize = %q, want 64m", got)
	}

	// Real drift stores the byte count.
	info.HostConfig.ShmSize = 96 << 20
	if diags := refreshShmSize(&state, info); diags.HasError() {
		t.Fatalf("refreshShmSize: %v", diags)
	}
	if got := state.ShmSize.ValueString(); got != "100663296" {
		t.Errorf("ShmSize = %q, want 100663296", got)
	}
}

func TestRefreshStopTimeoutAndPlatform(t *testing.T) {
	info := &containerInspect{}
	info.Config.Labels = map[string]string{
		"nerdctl/stop-timeout": "5",
		// The platform label is always present, normalized.
		"nerdctl/platform": "linux/amd64",
	}

	// stop_timeout fills from its label (import); unmanaged platform stays
	// null despite the ever-present label.
	state := minimalContainerModel()
	refreshStopTimeout(&state, info)
	refreshManagedString(&state.Platform, info.platform())
	if got := state.StopTimeout.ValueInt64(); got != 5 {
		t.Errorf("StopTimeout = %d, want 5", got)
	}
	if !state.Platform.IsNull() {
		t.Errorf("Platform = %v, want null", state.Platform)
	}

	// A managed platform tracks the normalized label value.
	state.Platform = types.StringValue("amd64")
	refreshManagedString(&state.Platform, info.platform())
	if got := state.Platform.ValueString(); got != "linux/amd64" {
		t.Errorf("Platform = %q, want linux/amd64", got)
	}

	// A missing stop-timeout label nulls the attribute.
	info.Config.Labels = map[string]string{}
	refreshStopTimeout(&state, info)
	if !state.StopTimeout.IsNull() {
		t.Errorf("StopTimeout = %v, want null", state.StopTimeout)
	}
}

func TestBuildUpdateArgs(t *testing.T) {
	base := func() (containerResourceModel, containerResourceModel) {
		plan := minimalContainerModel()
		state := minimalContainerModel()
		return plan, state
	}

	t.Run("nothing updatable changed", func(t *testing.T) {
		plan, state := base()
		plan.Wait = types.BoolValue(true) // wait alone needs no host call
		if args := buildUpdateArgs(&plan, &state); args != nil {
			t.Errorf("args = %v, want nil", args)
		}
	})

	t.Run("all three changed", func(t *testing.T) {
		plan, state := base()
		state.Memory = types.StringValue("64m")
		state.Cpus = types.Float64Value(0.25)
		plan.Memory = types.StringValue("96m")
		plan.Cpus = types.Float64Value(0.5)
		plan.Restart = types.StringValue("always")
		// cpus becomes quota/period: released nerdctl fails to persist --cpus.
		want := []string{"update", "--memory", "96m", "--cpu-quota", "50000", "--cpu-period", "100000", "--restart", "always", "app"}
		if args := buildUpdateArgs(&plan, &state); !reflect.DeepEqual(args, want) {
			t.Errorf("args = %v, want %v", args, want)
		}
	})

	t.Run("limit added where none existed", func(t *testing.T) {
		plan, state := base()
		plan.Memory = types.StringValue("64m")
		want := []string{"update", "--memory", "64m", "app"}
		if args := buildUpdateArgs(&plan, &state); !reflect.DeepEqual(args, want) {
			t.Errorf("args = %v, want %v", args, want)
		}
	})
}

func TestBuildRunArgsNoHealthcheck(t *testing.T) {
	plan := minimalContainerModel()
	plan.NoHealthcheck = types.BoolValue(true)

	args, diags := buildRunArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"run", "-d", "--name", "app", "--restart", "unless-stopped", "--log-driver", "json-file", "--no-healthcheck", "traefik:v3"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestRefreshPrivileged(t *testing.T) {
	info := &containerInspect{}
	info.HostConfig.Privileged = true

	// Drift onto the default is written.
	state := minimalContainerModel()
	refreshPrivileged(&state, info)
	if !state.Privileged.ValueBool() {
		t.Error("Privileged = false, want true")
	}

	// Null state (the import path) always gets a concrete value.
	state.Privileged = types.BoolNull()
	refreshPrivileged(&state, &containerInspect{})
	if state.Privileged.IsNull() || state.Privileged.ValueBool() {
		t.Errorf("Privileged = %v, want false", state.Privileged)
	}
}

func TestRefreshCaps(t *testing.T) {
	ctx := context.Background()

	// Inspect reconstructs OCI names; config uses CLI form. Order, case,
	// and the CAP_ prefix are all insignificant.
	l := mustList(t, types.StringType, []string{"sys_time", "NET_ADMIN"})
	if diags := refreshCaps(ctx, &l, []string{"CAP_NET_ADMIN", "CAP_SYS_TIME"}, false); diags.HasError() {
		t.Fatalf("refreshCaps: %v", diags)
	}
	var got []string
	if diags := l.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading caps: %v", diags)
	}
	if want := []string{"sys_time", "NET_ADMIN"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caps = %v, want untouched %v", got, want)
	}

	// Real drift rewrites in CLI form, sorted.
	if diags := refreshCaps(ctx, &l, []string{"CAP_SYS_TIME"}, false); diags.HasError() {
		t.Fatalf("refreshCaps: %v", diags)
	}
	got = nil
	if diags := l.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading caps: %v", diags)
	}
	if want := []string{"SYS_TIME"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caps = %v, want %v", got, want)
	}

	// Privileged containers hold every capability: never tracked.
	if diags := refreshCaps(ctx, &l, []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE"}, true); diags.HasError() {
		t.Fatalf("refreshCaps: %v", diags)
	}
	got = nil
	if diags := l.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("reading caps: %v", diags)
	}
	if want := []string{"SYS_TIME"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caps = %v, want untouched %v", got, want)
	}

	// Removed out-of-band nulls the list.
	if diags := refreshCaps(ctx, &l, nil, false); diags.HasError() {
		t.Fatalf("refreshCaps: %v", diags)
	}
	if !l.IsNull() {
		t.Errorf("caps = %v, want null", l)
	}
}

func TestRefreshSysctlsIgnoresInjectedDefault(t *testing.T) {
	// nerdctl injects net.ipv4.ip_unprivileged_port_start=0 into every
	// container; an unmanaged state must not pick it up.
	info := &containerInspect{}
	info.HostConfig.Sysctls = map[string]string{unprivilegedPortSysctl: "0"}
	state := minimalContainerModel()

	if diags := refreshSysctls(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshSysctls: %v", diags)
	}
	if !state.Sysctls.IsNull() {
		t.Errorf("Sysctls = %v, want null", state.Sysctls)
	}

	// When the config manages the key, it is kept and compared.
	state.Sysctls = mustMap(t, map[string]string{unprivilegedPortSysctl: "0"})
	if diags := refreshSysctls(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshSysctls: %v", diags)
	}
	got := map[string]string{}
	if diags := state.Sysctls.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading sysctls: %v", diags)
	}
	if want := map[string]string{unprivilegedPortSysctl: "0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sysctls = %v, want %v", got, want)
	}

	// User sysctls surface alongside the ignored injected one.
	info.HostConfig.Sysctls["net.core.somaxconn"] = "512"
	state = minimalContainerModel()
	if diags := refreshSysctls(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshSysctls: %v", diags)
	}
	got = map[string]string{}
	if diags := state.Sysctls.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading sysctls: %v", diags)
	}
	if want := map[string]string{"net.core.somaxconn": "512"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sysctls = %v, want %v", got, want)
	}
}

func TestRefreshTmpfs(t *testing.T) {
	// Inspect reports the merged option string; the configured spelling is
	// kept on a semantic match.
	info := &containerInspect{}
	info.HostConfig.Tmpfs = map[string]string{"/run": "nosuid,nodev,size=64m,exec"}
	state := minimalContainerModel()
	state.Tmpfs = mustMap(t, map[string]string{"/run": "size=64m,exec"})

	if diags := refreshTmpfs(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshTmpfs: %v", diags)
	}
	got := map[string]string{}
	if diags := state.Tmpfs.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading tmpfs: %v", diags)
	}
	if want := map[string]string{"/run": "size=64m,exec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tmpfs = %v, want untouched %v", got, want)
	}

	// A differing option set is drift and takes the inspect form.
	info.HostConfig.Tmpfs["/run"] = "noexec,nosuid,nodev,size=32m"
	if diags := refreshTmpfs(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshTmpfs: %v", diags)
	}
	got = map[string]string{}
	if diags := state.Tmpfs.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading tmpfs: %v", diags)
	}
	if want := map[string]string{"/run": "noexec,nosuid,nodev,size=32m"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tmpfs = %v, want %v", got, want)
	}

	// Null state and no tmpfs mounts compare equal.
	state = minimalContainerModel()
	if diags := refreshTmpfs(context.Background(), &state, &containerInspect{}); diags.HasError() {
		t.Fatalf("refreshTmpfs: %v", diags)
	}
	if !state.Tmpfs.IsNull() {
		t.Errorf("Tmpfs = %v, want null", state.Tmpfs)
	}
}

func TestRefreshLogConfig(t *testing.T) {
	// Old inspect output without a log config label means json-file; the
	// computed default matches and null opts stay null.
	state := minimalContainerModel()
	if diags := refreshLogConfig(context.Background(), &state, &containerInspect{}); diags.HasError() {
		t.Fatalf("refreshLogConfig: %v", diags)
	}
	if state.LogDriver.ValueString() != "json-file" {
		t.Errorf("LogDriver = %v, want json-file", state.LogDriver)
	}
	if !state.LogOpts.IsNull() {
		t.Errorf("LogOpts = %v, want null", state.LogOpts)
	}

	// The import path arrives null and gets the concrete driver.
	state.LogDriver = types.StringNull()
	info := &containerInspect{}
	info.HostConfig.LogConfig.Driver = "journald"
	info.HostConfig.LogConfig.Opts = map[string]string{"tag": "app"}
	if diags := refreshLogConfig(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshLogConfig: %v", diags)
	}
	if state.LogDriver.ValueString() != "journald" {
		t.Errorf("LogDriver = %v, want journald", state.LogDriver)
	}
	got := map[string]string{}
	if diags := state.LogOpts.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("reading log opts: %v", diags)
	}
	if want := map[string]string{"tag": "app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LogOpts = %v, want %v", got, want)
	}
}

func TestRefreshHealthcheckUnmanaged(t *testing.T) {
	// An image-defined healthcheck must not surface when the config does
	// not manage the block.
	info := &containerInspect{}
	info.Config.Healthcheck = &healthcheckInspect{
		Test: []string{"CMD-SHELL", "curl -f http://localhost/"}, Interval: int64(30 * time.Second),
		Timeout: int64(30 * time.Second), Retries: 3,
	}
	state := minimalContainerModel()

	if diags := refreshHealthcheck(context.Background(), &state, info); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if !state.Healthcheck.IsNull() {
		t.Errorf("Healthcheck = %v, want null", state.Healthcheck)
	}
}

func TestRefreshHealthcheckManaged(t *testing.T) {
	ctx := context.Background()
	info := &containerInspect{}
	info.Config.Healthcheck = &healthcheckInspect{
		Test:     []string{"CMD-SHELL", "true"},
		Interval: int64(90 * time.Second), Timeout: int64(30 * time.Second),
		StartPeriod: 0, Retries: 3,
	}

	// Semantically equal: "1m30s" is the same 90s and unset fields match
	// the create-time defaults, so the state object is untouched.
	state := minimalContainerModel()
	state.Healthcheck = mustHealthcheck(t, healthcheckModel{
		Command:  types.StringValue("true"),
		Interval: types.StringValue("1m30s"),
	})
	if diags := refreshHealthcheck(ctx, &state, info); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	var hc healthcheckModel
	if diags := state.Healthcheck.As(ctx, &hc, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("reading healthcheck: %v", diags)
	}
	if hc.Interval.ValueString() != "1m30s" {
		t.Errorf("Interval = %v, want 1m30s kept", hc.Interval)
	}
	if !hc.Retries.IsNull() {
		t.Errorf("Retries = %v, want null (matches default)", hc.Retries)
	}

	// Drift on one field rewrites it in canonical duration form.
	info.Config.Healthcheck.Interval = int64(45 * time.Second)
	info.Config.Healthcheck.Retries = 5
	if diags := refreshHealthcheck(ctx, &state, info); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if diags := state.Healthcheck.As(ctx, &hc, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("reading healthcheck: %v", diags)
	}
	if hc.Interval.ValueString() != "45s" {
		t.Errorf("Interval = %v, want 45s", hc.Interval)
	}
	if hc.Retries.ValueInt64() != 5 {
		t.Errorf("Retries = %v, want 5", hc.Retries)
	}
	if hc.Command.ValueString() != "true" {
		t.Errorf("Command = %v, want true", hc.Command)
	}

	// Healthcheck removed out-of-band nulls the block.
	if diags := refreshHealthcheck(ctx, &state, &containerInspect{}); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if !state.Healthcheck.IsNull() {
		t.Errorf("Healthcheck = %v, want null", state.Healthcheck)
	}
}

func TestRefreshHealthcheckDisabled(t *testing.T) {
	disabled := &containerInspect{}
	disabled.Config.Healthcheck = &healthcheckInspect{Test: []string{"NONE"}}

	// Managed no_healthcheck matching the container: kept.
	state := minimalContainerModel()
	state.NoHealthcheck = types.BoolValue(true)
	if diags := refreshHealthcheck(context.Background(), &state, disabled); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if !state.NoHealthcheck.ValueBool() {
		t.Errorf("NoHealthcheck = %v, want true", state.NoHealthcheck)
	}

	// Managed no_healthcheck but the container has a real check: drift.
	enabled := &containerInspect{}
	enabled.Config.Healthcheck = &healthcheckInspect{Test: []string{"CMD-SHELL", "true"}}
	if diags := refreshHealthcheck(context.Background(), &state, enabled); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if !state.NoHealthcheck.IsNull() {
		t.Errorf("NoHealthcheck = %v, want null", state.NoHealthcheck)
	}

	// A managed healthcheck block against a disabled container: nulled.
	state = minimalContainerModel()
	state.Healthcheck = mustHealthcheck(t, healthcheckModel{Command: types.StringValue("true")})
	if diags := refreshHealthcheck(context.Background(), &state, disabled); diags.HasError() {
		t.Fatalf("refreshHealthcheck: %v", diags)
	}
	if !state.Healthcheck.IsNull() {
		t.Errorf("Healthcheck = %v, want null", state.Healthcheck)
	}
}

func TestBuildRunArgsInvalidMounts(t *testing.T) {
	tests := []struct {
		name  string
		mount volumeMountModel
	}{
		{
			name: "both host_path and volume_name",
			mount: volumeMountModel{
				ContainerPath: types.StringValue("/data"),
				HostPath:      types.StringValue("/srv/data"),
				VolumeName:    types.StringValue("data"),
				ReadOnly:      types.BoolValue(false),
			},
		},
		{
			name: "neither host_path nor volume_name",
			mount: volumeMountModel{
				ContainerPath: types.StringValue("/data"),
				HostPath:      types.StringNull(),
				VolumeName:    types.StringNull(),
				ReadOnly:      types.BoolValue(false),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := minimalContainerModel()
			plan.Volumes = mustList(t, volumeObjectType, []volumeMountModel{tt.mount})

			_, diags := buildRunArgs(context.Background(), &plan)
			if !diags.HasError() {
				t.Fatal("expected diagnostics error, got none")
			}
			found := false
			for _, d := range diags.Errors() {
				if strings.Contains(d.Detail(), "exactly one of host_path or volume_name") {
					found = true
				}
			}
			if !found {
				t.Errorf("diagnostics missing mount validation error: %v", diags)
			}
		})
	}
}
