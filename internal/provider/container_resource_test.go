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
		Name:    types.StringValue("app"),
		Image:   types.StringValue("traefik:v3"),
		Restart: types.StringValue("unless-stopped"),
		Command: types.ListNull(types.StringType),
		Ports:   types.ListNull(portObjectType),
		Labels:  types.MapNull(types.StringType),
		Volumes: types.ListNull(volumeObjectType),
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
		"-p", "8080:80/tcp",
		"-p", "69:69/udp",
		"--label", "a.label=1", // map keys must come out sorted, not in map order
		"--label", "b.label=2",
		"-v", "/srv/app:/etc/app:ro",
		"-v", "app_config:/data",
		"traefik:v3",
		"--flag=value", "serve",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
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
