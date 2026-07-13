package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// minimalImageModel mirrors a pull-only plan: everything optional is null.
func minimalImageModel() imageResourceModel {
	return imageResourceModel{
		Name:     types.StringValue("alpine:3.20"),
		Platform: types.StringNull(),
		Triggers: types.MapNull(types.StringType),
		Build:    types.ObjectNull(imageBuildObjectType.AttrTypes),
	}
}

func mustImageBuild(t *testing.T, b imageBuildModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(context.Background(), imageBuildObjectType.AttrTypes, b)
	if diags.HasError() {
		t.Fatalf("building build object: %v", diags)
	}
	return obj
}

func TestImagePullArgsMinimal(t *testing.T) {
	plan := minimalImageModel()

	args := imagePullArgs(&plan)
	want := []string{"pull", "--quiet", "alpine:3.20"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestImagePullArgsPlatform(t *testing.T) {
	plan := minimalImageModel()
	plan.Platform = types.StringValue("linux/arm64")

	args := imagePullArgs(&plan)
	want := []string{"pull", "--quiet", "--platform", "linux/arm64", "alpine:3.20"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestImageBuildArgsMinimal(t *testing.T) {
	plan := minimalImageModel()
	plan.Name = types.StringValue("app:dev")
	plan.Build = mustImageBuild(t, imageBuildModel{
		Context:    types.StringValue("/srv/app"),
		Dockerfile: types.StringNull(),
		Target:     types.StringNull(),
		BuildArgs:  types.MapNull(types.StringType),
		Labels:     types.MapNull(types.StringType),
		NoCache:    types.BoolNull(),
	})

	args, diags := imageBuildArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"build", "-t", "app:dev", "/srv/app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestImageBuildArgsFull(t *testing.T) {
	plan := minimalImageModel()
	plan.Name = types.StringValue("app:dev")
	plan.Platform = types.StringValue("linux/amd64")
	plan.Build = mustImageBuild(t, imageBuildModel{
		Context:    types.StringValue("/srv/app"),
		Dockerfile: types.StringValue("/srv/app/Dockerfile.prod"),
		Target:     types.StringValue("release"),
		BuildArgs:  mustMap(t, map[string]string{"VERSION": "1.2.3", "BASE": "alpine"}),
		Labels:     mustMap(t, map[string]string{"team": "infra"}),
		NoCache:    types.BoolValue(true),
	})

	args, diags := imageBuildArgs(context.Background(), &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{
		"build", "-t", "app:dev",
		"--platform", "linux/amd64",
		"-f", "/srv/app/Dockerfile.prod",
		"--target", "release",
		"--build-arg", "BASE=alpine", "--build-arg", "VERSION=1.2.3",
		"--label", "team=infra",
		"--no-cache",
		"/srv/app",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestFirstRepoDigest(t *testing.T) {
	tests := []struct {
		name    string
		digests []string
		want    string
	}{
		{"pulled", []string{"docker.io/library/alpine@sha256:abc"}, "docker.io/library/alpine@sha256:abc"},
		{"no repo name", nil, ""},
		{"none placeholder", []string{"<none>@<none>"}, ""},
		{"placeholder then real", []string{"<none>@<none>", "registry.local/app@sha256:def"}, "registry.local/app@sha256:def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstRepoDigest(tt.digests); got != tt.want {
				t.Errorf("firstRepoDigest(%v) = %q, want %q", tt.digests, got, tt.want)
			}
		})
	}
}
