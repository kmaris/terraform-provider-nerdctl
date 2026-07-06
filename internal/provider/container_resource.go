package provider

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var restartPolicyRe = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:\d+)?)$`)

var (
	_ resource.Resource                = (*containerResource)(nil)
	_ resource.ResourceWithConfigure   = (*containerResource)(nil)
	_ resource.ResourceWithImportState = (*containerResource)(nil)
)

var (
	portObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"internal": types.Int64Type,
		"external": types.Int64Type,
		"protocol": types.StringType,
	}}
	volumeObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"container_path": types.StringType,
		"host_path":      types.StringType,
		"volume_name":    types.StringType,
		"read_only":      types.BoolType,
	}}
)

func NewContainerResource() resource.Resource { return &containerResource{} }

type containerResource struct {
	client *nerdctl.Client
}

type containerResourceModel struct {
	Name     types.String `tfsdk:"name"`
	Image    types.String `tfsdk:"image"`
	Command  types.List   `tfsdk:"command"`
	Restart  types.String `tfsdk:"restart"`
	Networks types.List   `tfsdk:"networks"`
	Ports    types.List   `tfsdk:"ports"`
	Labels   types.Map    `tfsdk:"labels"`
	Volumes  types.List   `tfsdk:"volumes"`
	ID       types.String `tfsdk:"id"`
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
				Description:   "Restart policy handled by containerd's restart manager: `no`, `always`, `unless-stopped`, or `on-failure[:max-retries]`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(restartPolicyRe, "must be no, always, unless-stopped, or on-failure[:max-retries]"),
				},
			},
			"networks": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Networks to attach, e.g. `nerdctl_network` names. Runs on the default bridge when unset.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
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
							Required:   true,
							Validators: []validator.Int64{int64validator.Between(1, 65535)},
						},
						"external": schema.Int64Attribute{
							Required:   true,
							Validators: []validator.Int64{int64validator.Between(1, 65535)},
						},
						"protocol": schema.StringAttribute{
							Optional:   true,
							Computed:   true,
							Default:    stringdefault.StaticString("tcp"),
							Validators: []validator.String{stringvalidator.OneOf("tcp", "udp", "sctp")},
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
							Validators: []validator.String{
								stringvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("volume_name")),
							},
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

	out, err := r.client.Run(ctx, "container", "inspect", state.Name.ValueString())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect container", err.Error())
		return
	}

	info, err := parseContainerInspect(out)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse container inspect output", err.Error())
		return
	}

	state.ID = types.StringValue(info.ID)

	if normalizeImageRef(info.Image) != normalizeImageRef(state.Image.ValueString()) {
		state.Image = types.StringValue(normalizeImageRef(info.Image))
	}

	if policy := info.restartPolicy(); policy != state.Restart.ValueString() {
		state.Restart = types.StringValue(policy)
	}

	// command is deliberately left untouched: the OCI spec merges entrypoint
	// and command, so it cannot be recovered from inspect output.

	// Image labels merge into container labels; fetch them so they can be
	// subtracted. Best-effort: without them (image removed out-of-band),
	// image-defined labels would surface as drift.
	imageLabels := map[string]string{}
	if imgOut, err := r.client.Run(ctx, "image", "inspect", info.Image); err == nil {
		if parsed, err := parseImageLabels(imgOut); err == nil {
			imageLabels = parsed
		}
	}

	resp.Diagnostics.Append(refreshLabels(ctx, &state, info, imageLabels)...)
	resp.Diagnostics.Append(refreshNetworks(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshPorts(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshVolumes(ctx, &state, info)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// refreshLabels overwrites state labels when the container's user labels
// differ. State is kept as-is on a semantic match so null vs empty and
// map ordering never show as drift.
func refreshLabels(ctx context.Context, state *containerResourceModel, info *containerInspect, imageLabels map[string]string) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.userLabels(imageLabels)

	current := map[string]string{}
	if !state.Labels.IsNull() {
		diags.Append(state.Labels.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Labels = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Labels = m
	return diags
}

// refreshNetworks overwrites state networks when the container's attached
// networks differ. A null state matches the default bridge, so unconfigured
// containers on the default network never show drift. Order is significant:
// it determines interface order in the container.
func refreshNetworks(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.networks()

	if state.Networks.IsNull() && (len(actual) == 0 || (len(actual) == 1 && actual[0] == "bridge")) {
		return diags
	}
	var current []string
	if !state.Networks.IsNull() {
		diags.Append(state.Networks.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if slices.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Networks = types.ListNull(types.StringType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Networks = l
	return diags
}

func refreshPorts(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual, err := info.portModels()
	if err != nil {
		diags.AddError("Failed to parse container ports", err.Error())
		return diags
	}

	var current []portModel
	if !state.Ports.IsNull() {
		diags.Append(state.Ports.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if portSetsEqual(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Ports = types.ListNull(portObjectType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, portObjectType, actual)
	diags.Append(d...)
	state.Ports = l
	return diags
}

func refreshVolumes(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.volumeMounts()

	var current []volumeMountModel
	if !state.Volumes.IsNull() {
		diags.Append(state.Volumes.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if mountSetsEqual(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Volumes = types.ListNull(volumeObjectType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, volumeObjectType, actual)
	diags.Append(d...)
	state.Volumes = l
	return diags
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

// ImportState imports by container name, e.g.
// `terraform import nerdctl_container.app app`. Read recovers every
// attribute except command, which is not present in inspect output — set it
// in config to match the running container before applying.
func (r *containerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func buildRunArgs(ctx context.Context, plan *containerResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	args := []string{"run", "-d", "--name", plan.Name.ValueString()}

	if restart := plan.Restart.ValueString(); restart != "" {
		args = append(args, "--restart", restart)
	}

	if !plan.Networks.IsNull() {
		var networks []string
		diags.Append(plan.Networks.ElementsAs(ctx, &networks, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, n := range networks {
			args = append(args, "--net", n)
		}
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
