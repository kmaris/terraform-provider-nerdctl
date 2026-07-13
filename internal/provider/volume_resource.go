package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource                = (*volumeResource)(nil)
	_ resource.ResourceWithConfigure   = (*volumeResource)(nil)
	_ resource.ResourceWithImportState = (*volumeResource)(nil)
)

// NewVolumeResource returns the nerdctl_volume resource.
func NewVolumeResource() resource.Resource { return &volumeResource{} }

type volumeResource struct {
	client *nerdctl.Client
}

type volumeResourceModel struct {
	Name       types.String `tfsdk:"name"`
	Labels     types.Map    `tfsdk:"labels"`
	Mountpoint types.String `tfsdk:"mountpoint"`
}

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named volume managed by nerdctl.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Volume name, unique on the host.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"labels": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Labels applied with `--label`.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"mountpoint": schema.StringAttribute{
				Computed:      true,
				Description:   "Directory on the host backing the volume.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *volumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args, diags := appendMapFlags(ctx, []string{"volume", "create"}, nil, "--label", plan.Labels)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	args = append(args, plan.Name.ValueString())

	if _, err := r.client.Run(ctx, args...); err != nil {
		resp.Diagnostics.AddError("Failed to create volume", err.Error())
		return
	}

	info, err := r.inspect(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect volume after create", err.Error())
		return
	}
	plan.Mountpoint = types.StringValue(info.Mountpoint)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info, err := r.inspect(ctx, state.Name.ValueString())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect volume", err.Error())
		return
	}
	state.Mountpoint = types.StringValue(info.Mountpoint)

	actual := stripNerdctlLabels(info.Labels)
	current := map[string]string{}
	if !state.Labels.IsNull() {
		resp.Diagnostics.Append(state.Labels.ElementsAs(ctx, &current, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if !maps.Equal(current, actual) {
		if len(actual) == 0 {
			state.Labels = types.MapNull(types.StringType)
		} else {
			m, d := types.MapValueFrom(ctx, types.StringType, actual)
			resp.Diagnostics.Append(d...)
			if resp.Diagnostics.HasError() {
				return
			}
			state.Labels = m
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: every attribute requires replacement.
func (r *volumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.Run(ctx, "volume", "rm", state.Name.ValueString()); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove volume", err.Error())
	}
}

// ImportState imports by volume name, e.g.
// `terraform import nerdctl_volume.config app_config`.
func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *volumeResource) inspect(ctx context.Context, name string) (*volumeInspect, error) {
	return inspectVolume(ctx, r.client, name)
}

// volumeInspect is the subset of `nerdctl volume inspect` output the
// provider reads.
type volumeInspect struct {
	Mountpoint string            `json:"Mountpoint"`
	Labels     map[string]string `json:"Labels"`
}

// inspectVolume looks up a volume, shared by the resource and data source.
func inspectVolume(ctx context.Context, client *nerdctl.Client, name string) (*volumeInspect, error) {
	out, err := client.Run(ctx, "volume", "inspect", name)
	if err != nil {
		return nil, err
	}
	var infos []volumeInspect
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parsing volume inspect output: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("volume %s: empty inspect result", name)
	}
	return &infos[0], nil
}
