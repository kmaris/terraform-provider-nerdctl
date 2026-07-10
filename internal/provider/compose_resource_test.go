package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestComposeBaseArgs(t *testing.T) {
	ctx := context.Background()
	r := &composeResource{}
	m := &composeResourceModel{
		ProjectName:      types.StringValue("app"),
		ConfigPaths:      mustList(t, types.StringType, []string{"/opt/app/compose.yaml", "/opt/app/override.yaml"}),
		ProjectDirectory: types.StringValue("/opt/app"),
		EnvFiles:         mustList(t, types.StringType, []string{"/opt/app/.env"}),
		Profiles:         mustList(t, types.StringType, []string{"prod"}),
	}

	args, diags := r.composeBaseArgs(ctx, m)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{
		"compose", "-p", "app",
		"-f", "/opt/app/compose.yaml",
		"-f", "/opt/app/override.yaml",
		"--project-directory", "/opt/app",
		"--env-file", "/opt/app/.env",
		"--profile", "prod",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestComposeBaseArgsMinimal(t *testing.T) {
	ctx := context.Background()
	r := &composeResource{}
	m := &composeResourceModel{
		ProjectName:      types.StringValue("app"),
		ConfigPaths:      mustList(t, types.StringType, []string{"/c.yaml"}),
		ProjectDirectory: types.StringNull(),
		EnvFiles:         types.ListNull(types.StringType),
		Profiles:         types.ListNull(types.StringType),
	}

	args, diags := r.composeBaseArgs(ctx, m)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"compose", "-p", "app", "-f", "/c.yaml"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestDeriveComposeProjectName(t *testing.T) {
	tests := []struct{ dir, want string }{
		{"/opt/myapp", "myapp"},
		{"/opt/My-App_1/", "my-app_1"}, // lowercased, trailing slash trimmed
		{"/opt/App!@#", "app"},         // invalid characters dropped
		{"/srv/_hidden", "hidden"},     // leading separators trimmed
		{"relativename", "relativename"},
		{"", "compose"}, // fallback
		{"/", "compose"},
	}
	for _, tt := range tests {
		if got := deriveComposeProjectName(tt.dir); got != tt.want {
			t.Errorf("deriveComposeProjectName(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestResolveProjectName(t *testing.T) {
	ctx := context.Background()
	r := &composeResource{}

	// An explicit name is kept.
	m := &composeResourceModel{
		ProjectName: types.StringValue("explicit"),
		ConfigPaths: mustList(t, types.StringType, []string{"/opt/app/c.yaml"}),
	}
	if diags := r.resolveProjectName(ctx, m); diags.HasError() {
		t.Fatalf("resolveProjectName: %v", diags)
	}
	if m.ProjectName.ValueString() != "explicit" {
		t.Errorf("ProjectName = %q, want explicit", m.ProjectName.ValueString())
	}

	// Omitted: derive from project_directory.
	m = &composeResourceModel{
		ProjectName:      types.StringNull(),
		ProjectDirectory: types.StringValue("/srv/webstack"),
		ConfigPaths:      mustList(t, types.StringType, []string{"/x/c.yaml"}),
	}
	if diags := r.resolveProjectName(ctx, m); diags.HasError() {
		t.Fatalf("resolveProjectName: %v", diags)
	}
	if m.ProjectName.ValueString() != "webstack" {
		t.Errorf("ProjectName = %q, want webstack", m.ProjectName.ValueString())
	}

	// Omitted with no project_directory: derive from the first config path.
	m = &composeResourceModel{
		ProjectName:      types.StringNull(),
		ProjectDirectory: types.StringNull(),
		ConfigPaths:      mustList(t, types.StringType, []string{"/opt/myapp/docker-compose.yml"}),
	}
	if diags := r.resolveProjectName(ctx, m); diags.HasError() {
		t.Fatalf("resolveProjectName: %v", diags)
	}
	if m.ProjectName.ValueString() != "myapp" {
		t.Errorf("ProjectName = %q, want myapp", m.ProjectName.ValueString())
	}

	// Unknown (computed at plan time) also derives.
	m = &composeResourceModel{
		ProjectName:      types.StringUnknown(),
		ProjectDirectory: types.StringValue("/srv/blog"),
		ConfigPaths:      mustList(t, types.StringType, []string{"/x/c.yaml"}),
	}
	if diags := r.resolveProjectName(ctx, m); diags.HasError() {
		t.Fatalf("resolveProjectName: %v", diags)
	}
	if m.ProjectName.ValueString() != "blog" {
		t.Errorf("ProjectName = %q, want blog", m.ProjectName.ValueString())
	}
}

func TestParseComposeServices(t *testing.T) {
	got := parseComposeServices("web\n\ndb\n  cache  \n")
	if want := []string{"web", "db", "cache"}; !reflect.DeepEqual(got, want) {
		t.Errorf("parseComposeServices = %v, want %v", got, want)
	}
	if got := parseComposeServices("   \n\n"); len(got) != 0 {
		t.Errorf("parseComposeServices(blank) = %v, want empty", got)
	}
}
