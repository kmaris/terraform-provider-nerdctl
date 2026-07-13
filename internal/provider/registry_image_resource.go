package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource              = (*registryImageResource)(nil)
	_ resource.ResourceWithConfigure = (*registryImageResource)(nil)
)

// NewRegistryImageResource returns the nerdctl_registry_image resource.
func NewRegistryImageResource() resource.Resource { return &registryImageResource{} }

type registryImageResource struct {
	client *nerdctl.Client
}

// registryImageResourceModel pushes a local image to its registry and tracks
// the remote manifest with `nerdctl manifest inspect`, which queries the
// registry without pulling. Requires nerdctl >= 2.3.
type registryImageResourceModel struct {
	Name             types.String `tfsdk:"name"`
	Platform         types.String `tfsdk:"platform"`
	AllPlatforms     types.Bool   `tfsdk:"all_platforms"`
	InsecureRegistry types.Bool   `tfsdk:"insecure_registry"`
	Triggers         types.Map    `tfsdk:"triggers"`
	SHA256Digest     types.String `tfsdk:"sha256_digest"`
	ID               types.String `tfsdk:"id"`
}

func (r *registryImageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_image"
}

func (r *registryImageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A local image pushed to its registry with `nerdctl push`. Refresh checks the remote manifest (`nerdctl manifest inspect`, requires nerdctl >= 2.3): an image deleted from the registry is re-pushed on the next apply. Registry credentials come from the host's `nerdctl login` state. Destroy removes only the Terraform state; nerdctl cannot delete from a registry.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Image reference to push, e.g. `registry.example.com/app:v1`. The image must exist locally under this reference — typically a `nerdctl_image` name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"platform": schema.StringAttribute{
				Optional:      true,
				Description:   "Push only this platform of a multi-platform image, passed with `--platform`. Conflicts with `all_platforms`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("all_platforms")),
				},
			},
			"all_platforms": schema.BoolAttribute{
				Optional:      true,
				Description:   "Push every platform of a multi-platform image, passed with `--all-platforms`.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"insecure_registry": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow plain-HTTP communication with the registry, for pushes (`--insecure-registry`) and manifest checks (`--insecure`).",
			},
			"triggers": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Arbitrary values whose change forces a re-push (replacement). Key it on the source image's `id` so a rebuilt or re-pulled image is pushed again.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"sha256_digest": schema.StringAttribute{
				Computed:    true,
				Description: "Digest of the remote manifest, e.g. `sha256:...`. For multi-platform references, where the registry check cannot see the index itself, this is a stable digest computed over the per-platform manifest digests.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The image reference.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *registryImageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *registryImageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryImageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.Run(ctx, registryImagePushArgs(&plan)...); err != nil {
		resp.Diagnostics.AddError("Failed to push image", err.Error())
		return
	}

	digest, err := remoteManifestDigest(ctx, r.client, plan.Name.ValueString(), plan.InsecureRegistry.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect remote manifest after push", err.Error())
		return
	}
	plan.SHA256Digest = types.StringValue(digest)
	plan.ID = plan.Name

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read checks the remote manifest: a reference gone from the registry drops
// out of state so the next apply pushes again.
func (r *registryImageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryImageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	digest, err := remoteManifestDigest(ctx, r.client, state.Name.ValueString(), state.InsecureRegistry.ValueBool())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect remote manifest", err.Error())
		return
	}
	state.SHA256Digest = types.StringValue(digest)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever sees insecure_registry changes, which affect how the
// registry is contacted, not what was pushed.
func (r *registryImageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan registryImageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes only the Terraform state: nerdctl has no way to delete an
// image from a registry.
func (r *registryImageResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// registryImagePushArgs builds the `nerdctl push` argument list.
func registryImagePushArgs(m *registryImageResourceModel) []string {
	args := []string{"push", "--quiet"}
	if m.AllPlatforms.ValueBool() {
		args = append(args, "--all-platforms")
	}
	if p := m.Platform.ValueString(); p != "" {
		args = append(args, "--platform", p)
	}
	if m.InsecureRegistry.ValueBool() {
		args = append(args, "--insecure-registry")
	}
	return append(args, m.Name.ValueString())
}

// remoteManifestDigest queries the registry for ref's manifest digest via
// `nerdctl manifest inspect --verbose`, shared by the registry_image
// resource and data source.
func remoteManifestDigest(ctx context.Context, client *nerdctl.Client, ref string, insecure bool) (string, error) {
	args := []string{"manifest", "inspect", "--verbose"}
	if insecure {
		args = append(args, "--insecure")
	}
	out, err := client.Run(ctx, append(args, ref)...)
	if err != nil {
		return "", err
	}
	return parseManifestDigest(out)
}

// manifestEntry is the subset of nerdctl's verbose manifest inspect output
// (manifesttypes.DockerManifestEntry) the provider reads.
type manifestEntry struct {
	Ref        string `json:"Ref"`
	Descriptor struct {
		Digest string `json:"digest"`
	} `json:"Descriptor"`
}

// parseManifestDigest extracts a digest from `manifest inspect --verbose`
// output: a single object for one manifest, an array for a manifest list.
// The verbose output of a multi-platform reference lists the per-platform
// manifests but not the index that holds them, so its digest is synthesized
// from the child digests — stable, and changing whenever any child changes.
func parseManifestDigest(out string) (string, error) {
	var entry manifestEntry
	if err := json.Unmarshal([]byte(out), &entry); err == nil && entry.Descriptor.Digest != "" {
		return entry.Descriptor.Digest, nil
	}

	var entries []manifestEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return "", fmt.Errorf("parsing manifest inspect output: %w", err)
	}
	digests := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Descriptor.Digest == "" {
			return "", fmt.Errorf("manifest inspect output entry without a digest")
		}
		digests = append(digests, e.Descriptor.Digest)
	}
	if len(digests) == 0 {
		return "", fmt.Errorf("manifest inspect output holds no manifests")
	}
	if len(digests) == 1 {
		return digests[0], nil
	}
	sort.Strings(digests)
	sum := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
