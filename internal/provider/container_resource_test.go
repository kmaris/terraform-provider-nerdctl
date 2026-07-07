package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// minimalContainerModel mirrors a plan after defaults: restart is always set,
// everything optional is null.
func minimalContainerModel() containerResourceModel {
	return containerResourceModel{
		Name:      types.StringValue("app"),
		Image:     types.StringValue("traefik:v3"),
		Restart:   types.StringValue("unless-stopped"),
		Command:   types.ListNull(types.StringType),
		Networks:  types.ListNull(types.StringType),
		DNS:       types.ListNull(types.StringType),
		DNSOpts:   types.ListNull(types.StringType),
		DNSSearch: types.ListNull(types.StringType),
		Env:       types.MapNull(types.StringType),
		Ports:     types.ListNull(portObjectType),
		Labels:    types.MapNull(types.StringType),
		Volumes:   types.ListNull(volumeObjectType),
	}
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
	want := []string{"run", "-d", "--name", "app", "--restart", "unless-stopped", "traefik:v3"}
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
	plan.Networks = mustList(t, types.StringType, []string{"app-net", "other-net"})
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
		"--net", "app-net",
		"--net", "other-net",
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
