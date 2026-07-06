package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource              = (*containerResource)(nil)
	_ resource.ResourceWithConfigure = (*containerResource)(nil)
)

func NewContainerResource() resource.Resource { return &containerResource{} }

type containerResource struct {
	client *nerdctl.Client
}

type containerResourceModel struct {
	Name    types.String `tfsdk:"name"`
	Image   types.String `tfsdk:"image"`
	Command types.List   `tfsdk:"command"`
	Restart types.String `tfsdk:"restart"`
	Ports   types.List   `tfsdk:"ports"`
	Labels  types.Map    `tfsdk:"labels"`
	Volumes types.List   `tfsdk:"volumes"`
	ID      types.String `tfsdk:"id"`
}

type portModel struct {
	Internal types.Int64  `tfsdk:"internal"`
	External types.Int64  `tfsdk:"external"`
	Protocol types.String `tfsdk:"protocol"`
}

type volumeMountModel struct {
	ContainerPath types.String `tfsdk:"container_path"`
	HostPath      types.String `tfsdk:"host_path"`
	VolumeName    types.String `tfsdk:"volume_name"`
	ReadOnly      types.Bool   `tfsdk:"read_only"`
}

func (r *containerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (r *containerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A container run with `nerdctl run -d`. Containers are treated as immutable: every change forces a replacement.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"image": schema.StringAttribute{
				Required:      true,
				Description:   "Image reference. Reference a `nerdctl_image` name to order the pull first.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"command": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Command and arguments passed after the image.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"restart": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("unless-stopped"),
				Description:   "Restart policy handled by containerd's restart manager.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"labels": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"ports": schema.ListNestedAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"internal": schema.Int64Attribute{
							Required: true,
						},
						"external": schema.Int64Attribute{
							Required: true,
						},
						"protocol": schema.StringAttribute{
							Optional: true,
							Computed: true,
							Default:  stringdefault.StaticString("tcp"),
						},
					},
				},
			},
			"volumes": schema.ListNestedAttribute{
				Optional:      true,
				Description:   "Mounts. Set `host_path` for a bind mount or `volume_name` for a named volume, not both.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"container_path": schema.StringAttribute{
							Required: true,
						},
						"host_path": schema.StringAttribute{
							Optional: true,
						},
						"volume_name": schema.StringAttribute{
							Optional: true,
						},
						"read_only": schema.BoolAttribute{
							Optional: true,
							Computed: true,
							Default:  booldefault.StaticBool(false),
						},
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *containerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *containerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args, diags := buildRunArgs(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.Run(ctx, args...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run container", err.Error())
		return
	}
	plan.ID = types.StringValue(id)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Existence check only: run args aren't reliably recoverable from
	// inspect output, so attribute drift is not detected yet.
	out, err := r.client.Run(ctx, "container", "inspect", state.Name.ValueString())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect container", err.Error())
		return
	}

	var infos []struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal([]byte(out), &infos); err != nil || len(infos) == 0 {
		resp.Diagnostics.AddError("Failed to parse container inspect output", out)
		return
	}
	state.ID = types.StringValue(infos[0].ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: every attribute requires replacement.
func (r *containerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.Run(ctx, "rm", "-f", state.Name.ValueString()); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove container", err.Error())
	}
}

func buildRunArgs(ctx context.Context, plan *containerResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	args := []string{"run", "-d", "--name", plan.Name.ValueString()}

	if restart := plan.Restart.ValueString(); restart != "" {
		args = append(args, "--restart", restart)
	}

	if !plan.Ports.IsNull() {
		var ports []portModel
		diags.Append(plan.Ports.ElementsAs(ctx, &ports, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, p := range ports {
			spec := fmt.Sprintf("%d:%d/%s", p.External.ValueInt64(), p.Internal.ValueInt64(), p.Protocol.ValueString())
			args = append(args, "-p", spec)
		}
	}

	if !plan.Labels.IsNull() {
		labels := map[string]string{}
		diags.Append(plan.Labels.ElementsAs(ctx, &labels, false)...)
		if diags.HasError() {
			return nil, diags
		}
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--label", k+"="+labels[k])
		}
	}

	if !plan.Volumes.IsNull() {
		var mounts []volumeMountModel
		diags.Append(plan.Volumes.ElementsAs(ctx, &mounts, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, m := range mounts {
			hostPath := m.HostPath.ValueString()
			volumeName := m.VolumeName.ValueString()
			if (hostPath == "") == (volumeName == "") {
				diags.AddError(
					"Invalid volume mount",
					fmt.Sprintf("mount for %s: exactly one of host_path or volume_name must be set", m.ContainerPath.ValueString()),
				)
				return nil, diags
			}
			src := hostPath
			if src == "" {
				src = volumeName
			}
			spec := src + ":" + m.ContainerPath.ValueString()
			if m.ReadOnly.ValueBool() {
				spec += ":ro"
			}
			args = append(args, "-v", spec)
		}
	}

	args = append(args, plan.Image.ValueString())

	if !plan.Command.IsNull() {
		var command []string
		diags.Append(plan.Command.ElementsAs(ctx, &command, false)...)
		if diags.HasError() {
			return nil, diags
		}
		args = append(args, command...)
	}

	return args, diags
}
