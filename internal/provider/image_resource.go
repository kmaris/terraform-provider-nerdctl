package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource                = (*imageResource)(nil)
	_ resource.ResourceWithConfigure   = (*imageResource)(nil)
	_ resource.ResourceWithImportState = (*imageResource)(nil)
)

// imageBuildObjectType describes the build attribute, for constructing
// null/known object values.
var imageBuildObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"context":    types.StringType,
	"dockerfile": types.StringType,
	"target":     types.StringType,
	"build_args": types.MapType{ElemType: types.StringType},
	"labels":     types.MapType{ElemType: types.StringType},
	"no_cache":   types.BoolType,
}}

// NewImageResource returns the nerdctl_image resource.
func NewImageResource() resource.Resource { return &imageResource{} }

type imageResource struct {
	client *nerdctl.Client
}

type imageResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Platform    types.String `tfsdk:"platform"`
	KeepLocally types.Bool   `tfsdk:"keep_locally"`
	ForceRemove types.Bool   `tfsdk:"force_remove"`
	Triggers    types.Map    `tfsdk:"triggers"`
	Build       types.Object `tfsdk:"build"`
	RepoDigest  types.String `tfsdk:"repo_digest"`
	ID          types.String `tfsdk:"id"`
}

type imageBuildModel struct {
	Context    types.String `tfsdk:"context"`
	Dockerfile types.String `tfsdk:"dockerfile"`
	Target     types.String `tfsdk:"target"`
	BuildArgs  types.Map    `tfsdk:"build_args"`
	Labels     types.Map    `tfsdk:"labels"`
	NoCache    types.Bool   `tfsdk:"no_cache"`
}

func (r *imageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (r *imageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An image pulled or built with nerdctl. Pulls `name` from its registry, or builds it from `build.context` when `build` is set. Registry credentials come from the host's `nerdctl login` state.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Image reference, e.g. `traefik:v3`. The reference to pull, or the tag applied to the built image (`-t`) when `build` is set.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"platform": schema.StringAttribute{
				Optional:      true,
				Description:   "Target platform passed with `--platform`, e.g. `linux/arm64`. Uses the host platform when unset.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"keep_locally": schema.BoolAttribute{
				Optional:    true,
				Description: "Leave the image on the host on destroy, removing it from state only. Defaults to `false`.",
			},
			"force_remove": schema.BoolAttribute{
				Optional:    true,
				Description: "Remove with `rmi --force` on destroy, e.g. when other tags reference the same image. Defaults to `false`.",
			},
			"triggers": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Arbitrary values whose change forces a re-pull or rebuild (replacement). For builds, set e.g. a hash of the source files; for pulls, an upstream digest.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"build": schema.SingleNestedAttribute{
				Optional:      true,
				Description:   "Build the image with `nerdctl build` instead of pulling it. Requires a running buildkitd on the host. Sources are not tracked: use `triggers` to force rebuilds when they change.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"context": schema.StringAttribute{
						Required:    true,
						Description: "Build context directory on the host running nerdctl.",
						Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"dockerfile": schema.StringAttribute{
						Optional:    true,
						Description: "Dockerfile path on the host, passed with `-f`. Defaults to `Dockerfile` inside the context.",
					},
					"target": schema.StringAttribute{
						Optional:    true,
						Description: "Multi-stage build stage to stop at, passed with `--target`.",
					},
					"build_args": schema.MapAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Description: "Build-time variables passed with `--build-arg`.",
					},
					"labels": schema.MapAttribute{
						ElementType: types.StringType,
						Optional:    true,
						Description: "Labels applied to the image with `--label`.",
					},
					"no_cache": schema.BoolAttribute{
						Optional:    true,
						Description: "Build without the buildkit cache, passed as `--no-cache`. Defaults to `false`.",
					},
				},
			},
			"repo_digest": schema.StringAttribute{
				Computed:      true,
				Description:   "Immutable digest reference, e.g. `alpine@sha256:...`, usable as a container `image` on this host. Matches the registry digest for pulled images; for built images it resolves from a registry only after a push.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Image ID (digest) as reported by `nerdctl image inspect`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *imageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *imageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Build.IsNull() {
		if _, err := r.client.Run(ctx, imagePullArgs(&plan)...); err != nil {
			resp.Diagnostics.AddError("Failed to pull image", err.Error())
			return
		}
	} else {
		args, diags := imageBuildArgs(ctx, &plan)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if _, err := r.client.Run(ctx, args...); err != nil {
			resp.Diagnostics.AddError("Failed to build image", err.Error())
			return
		}
	}

	if err := r.refresh(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to inspect image after create", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.refresh(ctx, &state)
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect image", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever sees changes to the delete-time flags (keep_locally,
// force_remove): everything that affects the image itself requires
// replacement, so copying the plan into state is enough.
func (r *imageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan imageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state imageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.KeepLocally.ValueBool() {
		return
	}

	args := []string{"rmi"}
	if state.ForceRemove.ValueBool() {
		args = append(args, "--force")
	}
	args = append(args, state.Name.ValueString())
	if _, err := r.client.Run(ctx, args...); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove image", err.Error())
	}
}

// ImportState imports by image reference, e.g.
// `terraform import nerdctl_image.traefik traefik:v3`. Import cannot recover
// `build`, `platform`, or `triggers`; a config that sets them replaces the
// image on the next apply.
func (r *imageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// refresh updates the computed attributes from `nerdctl image inspect`.
func (r *imageResource) refresh(ctx context.Context, m *imageResourceModel) error {
	info, err := inspectImage(ctx, r.client, m.Name.ValueString())
	if err != nil {
		return err
	}
	m.ID = types.StringValue(info.ID)
	if info.RepoDigest == "" {
		m.RepoDigest = types.StringNull()
	} else {
		m.RepoDigest = types.StringValue(info.RepoDigest)
	}
	return nil
}

// imagePullArgs builds the `nerdctl pull` argument list for the model.
func imagePullArgs(m *imageResourceModel) []string {
	args := []string{"pull", "--quiet"}
	if p := m.Platform.ValueString(); p != "" {
		args = append(args, "--platform", p)
	}
	return append(args, m.Name.ValueString())
}

// imageBuildArgs builds the `nerdctl build` argument list for the model.
// Map-driven flags are sorted by key so the command line is deterministic.
func imageBuildArgs(ctx context.Context, m *imageResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	var b imageBuildModel
	diags.Append(m.Build.As(ctx, &b, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}

	args := []string{"build", "-t", m.Name.ValueString()}
	if p := m.Platform.ValueString(); p != "" {
		args = append(args, "--platform", p)
	}
	if f := b.Dockerfile.ValueString(); f != "" {
		args = append(args, "-f", f)
	}
	if t := b.Target.ValueString(); t != "" {
		args = append(args, "--target", t)
	}
	args, diags = appendMapFlags(ctx, args, diags, "--build-arg", b.BuildArgs)
	if diags.HasError() {
		return nil, diags
	}
	args, diags = appendMapFlags(ctx, args, diags, "--label", b.Labels)
	if diags.HasError() {
		return nil, diags
	}
	if b.NoCache.ValueBool() {
		args = append(args, "--no-cache")
	}
	return append(args, b.Context.ValueString()), diags
}

// imageInfo is the subset of `nerdctl image inspect` the provider tracks.
type imageInfo struct {
	ID         string
	RepoDigest string // empty when the image has no digest reference
}

// inspectImage looks up an image's digests, shared by the resource and data
// source.
func inspectImage(ctx context.Context, client *nerdctl.Client, name string) (imageInfo, error) {
	out, err := client.Run(ctx, "image", "inspect", name)
	if err != nil {
		return imageInfo{}, err
	}
	var infos []struct {
		ID          string   `json:"Id"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return imageInfo{}, fmt.Errorf("parsing image inspect output: %w", err)
	}
	if len(infos) == 0 {
		return imageInfo{}, fmt.Errorf("image %s: empty inspect result", name)
	}
	return imageInfo{ID: infos[0].ID, RepoDigest: firstRepoDigest(infos[0].RepoDigests)}, nil
}

// firstRepoDigest picks the first usable digest reference. containerd
// assigns digests at build time, so even unpushed images have one; only
// images without a repo name (e.g. loaded from an archive by ID) report
// none, or a "<none>@<none>" placeholder.
func firstRepoDigest(digests []string) string {
	for _, d := range digests {
		if d != "" && !strings.Contains(d, "<none>") {
			return d
		}
	}
	return ""
}
