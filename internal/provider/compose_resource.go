package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

// composeProjectNameRe matches the names nerdctl accepts for a compose
// project: lowercase alphanumerics, dashes, and underscores.
var composeProjectNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

var (
	_ resource.Resource                = (*composeResource)(nil)
	_ resource.ResourceWithConfigure   = (*composeResource)(nil)
	_ resource.ResourceWithImportState = (*composeResource)(nil)
)

// NewComposeResource returns the nerdctl_compose resource.
func NewComposeResource() resource.Resource { return &composeResource{} }

type composeResource struct {
	client *nerdctl.Client
}

// composeResourceModel manages a compose project as a unit. The compose files
// on the host are the source of truth for the services; the provider drives
// their lifecycle with `nerdctl compose up`/`down` and lets compose reconcile
// individual services, rather than tracking each one as Terraform state.
type composeResourceModel struct {
	ProjectName      types.String `tfsdk:"project_name"`
	ConfigPaths      types.List   `tfsdk:"config_paths"`
	ProjectDirectory types.String `tfsdk:"project_directory"`
	EnvFiles         types.List   `tfsdk:"env_files"`
	Profiles         types.List   `tfsdk:"profiles"`
	Build            types.Bool   `tfsdk:"build"`
	RemoveOrphans    types.Bool   `tfsdk:"remove_orphans"`
	RemoveVolumes    types.Bool   `tfsdk:"remove_volumes"`
	Services         types.List   `tfsdk:"services"`
	ID               types.String `tfsdk:"id"`
}

func (r *composeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compose"
}

func (r *composeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A compose project managed with `nerdctl compose up`/`down`. The compose files are read from the host; compose reconciles the individual services on each apply.",
		Attributes: map[string]schema.Attribute{
			"project_name": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Compose project name, unique on the host; namespaces the project's containers, networks, and volumes. Derived from `project_directory` (or the first `config_paths` entry's directory) when omitted, then pinned.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(composeProjectNameRe, "must be lowercase alphanumeric, optionally with dashes or underscores"),
				},
			},
			"config_paths": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Paths to compose files on the host, passed with `-f` in order (the docker_compose `config_paths` equivalent). Changing them re-runs `compose up` to reconcile.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"project_directory": schema.StringAttribute{
				Optional:      true,
				Description:   "Working directory for relative paths in the compose files (build contexts, bind mounts), passed with `--project-directory`. Defaults to the first file's directory.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"env_files": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Paths to env files on the host, each passed with `--env-file`.",
			},
			"profiles": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Compose profiles to enable, passed with `--profile`.",
			},
			"build": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Build images before starting, passed as `--build` to `compose up`.",
			},
			"remove_orphans": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Remove containers for services not defined in the compose files, passed as `--remove-orphans`.",
			},
			"remove_volumes": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "On destroy, also remove named volumes declared in the compose files, passing `--volumes` to `compose down`.",
			},
			"services": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Service names declared in the compose files, as reported by `compose config --services`.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The project name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *composeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *composeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan composeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.resolveProjectName(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.up(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.ProjectName
	resp.Diagnostics.Append(r.refreshServices(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *composeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state composeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	base, diags := r.composeBaseArgs(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A project with no running containers has been torn down out of band;
	// drop it from state so the next apply recreates it.
	out, err := r.client.Run(ctx, append(base, "ps", "-q")...)
	if err != nil {
		if nerdctl.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read compose project", err.Error())
		return
	}
	if strings.TrimSpace(out) == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(r.refreshServices(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *composeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan composeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(r.resolveProjectName(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// compose up reconciles the project: it recreates changed services and
	// leaves unchanged ones running.
	resp.Diagnostics.Append(r.up(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.ProjectName
	resp.Diagnostics.Append(r.refreshServices(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *composeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state composeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	base, diags := r.composeBaseArgs(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	args := append(base, "down")
	if state.RemoveVolumes.ValueBool() {
		args = append(args, "--volumes")
	}
	if state.RemoveOrphans.ValueBool() {
		args = append(args, "--remove-orphans")
	}
	if _, err := r.client.Run(ctx, args...); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove compose project", err.Error())
	}
}

// ImportState imports by project name. Read cannot recover the compose file
// paths, so set `config_paths` in config to match before applying.
func (r *composeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("project_name"), req, resp)
}

// up runs `compose up -d` with the configured build and orphan options.
func (r *composeResource) up(ctx context.Context, m *composeResourceModel) diag.Diagnostics {
	base, diags := r.composeBaseArgs(ctx, m)
	if diags.HasError() {
		return diags
	}
	args := append(base, "up", "-d")
	if m.Build.ValueBool() {
		args = append(args, "--build")
	}
	if m.RemoveOrphans.ValueBool() {
		args = append(args, "--remove-orphans")
	}
	if _, err := r.client.Run(ctx, args...); err != nil {
		diags.AddError("Failed to bring up compose project", err.Error())
	}
	return diags
}

// composeBaseArgs builds the arguments shared by every compose subcommand:
// the project name, the `-f` file list, and the project-directory, env-file,
// and profile options.
func (r *composeResource) composeBaseArgs(ctx context.Context, m *composeResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	args := []string{"compose", "-p", m.ProjectName.ValueString()}

	var configPaths []string
	diags.Append(m.ConfigPaths.ElementsAs(ctx, &configPaths, false)...)
	if diags.HasError() {
		return nil, diags
	}
	for _, f := range configPaths {
		args = append(args, "-f", f)
	}
	if d := m.ProjectDirectory.ValueString(); d != "" {
		args = append(args, "--project-directory", d)
	}
	if !m.EnvFiles.IsNull() {
		var envFiles []string
		diags.Append(m.EnvFiles.ElementsAs(ctx, &envFiles, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, e := range envFiles {
			args = append(args, "--env-file", e)
		}
	}
	if !m.Profiles.IsNull() {
		var profiles []string
		diags.Append(m.Profiles.ElementsAs(ctx, &profiles, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, p := range profiles {
			args = append(args, "--profile", p)
		}
	}
	return args, diags
}

// refreshServices records the project's service names from
// `compose config --services`.
func (r *composeResource) refreshServices(ctx context.Context, m *composeResourceModel) diag.Diagnostics {
	base, diags := r.composeBaseArgs(ctx, m)
	if diags.HasError() {
		return diags
	}
	out, err := r.client.Run(ctx, append(base, "config", "--services")...)
	if err != nil {
		diags.AddError("Failed to list compose services", err.Error())
		return diags
	}
	list, d := types.ListValueFrom(ctx, types.StringType, parseComposeServices(out))
	diags.Append(d...)
	m.Services = list
	return diags
}

// parseComposeServices splits the newline-separated output of
// `compose config --services` into service names.
func parseComposeServices(out string) []string {
	var services []string
	for _, s := range strings.Split(out, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			services = append(services, s)
		}
	}
	return services
}

// resolveProjectName fills in a derived project name when the config omits
// one, mirroring compose's default of naming the project after its directory.
// A name that is already set (explicitly or pinned in prior state) is left
// untouched.
func (r *composeResource) resolveProjectName(ctx context.Context, m *composeResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !m.ProjectName.IsNull() && !m.ProjectName.IsUnknown() {
		return diags
	}
	dir := m.ProjectDirectory.ValueString()
	if dir == "" {
		var configPaths []string
		diags.Append(m.ConfigPaths.ElementsAs(ctx, &configPaths, false)...)
		if diags.HasError() {
			return diags
		}
		if len(configPaths) > 0 {
			dir = composeParentDir(configPaths[0])
		}
	}
	m.ProjectName = types.StringValue(deriveComposeProjectName(dir))
	return diags
}

// deriveComposeProjectName normalizes a directory name into a compose project
// name: its base element, lowercased, keeping only [a-z0-9_-], with a fallback
// for an empty result.
func deriveComposeProjectName(dir string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(composeBaseName(dir)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	if name := strings.TrimLeft(b.String(), "_-"); name != "" {
		return name
	}
	return "compose"
}

// composeParentDir and composeBaseName split a slash-separated host path.
// They deliberately avoid path/filepath: the paths belong to the target host,
// whose separator may differ from the machine running the provider.
func composeParentDir(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func composeBaseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
