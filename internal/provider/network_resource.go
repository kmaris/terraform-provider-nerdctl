package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource                = (*networkResource)(nil)
	_ resource.ResourceWithConfigure   = (*networkResource)(nil)
	_ resource.ResourceWithImportState = (*networkResource)(nil)
)

func NewNetworkResource() resource.Resource { return &networkResource{} }

type networkResource struct {
	client *nerdctl.Client
}

type networkResourceModel struct {
	Name    types.String `tfsdk:"name"`
	Driver  types.String `tfsdk:"driver"`
	Subnet  types.String `tfsdk:"subnet"`
	Gateway types.String `tfsdk:"gateway"`
	Labels  types.Map    `tfsdk:"labels"`
	ID      types.String `tfsdk:"id"`
}

// networkInspect is the subset of `nerdctl network inspect` output
// (dockercompat) the provider reads. The driver is not reported.
type networkInspect struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
	IPAM struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
	Labels map[string]string `json:"Labels"`
}

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A CNI network created with `nerdctl network create`. Networks are immutable: every change forces a replacement.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"driver": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("bridge"),
				Description:   "Network driver. Not reported by `network inspect`, so drift is not detected and imports assume `bridge`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf("bridge", "macvlan", "ipvlan")},
			},
			"subnet": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Subnet in CIDR notation, e.g. `10.5.0.0/24`. Auto-assigned by nerdctl when unset.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"gateway": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Gateway address within `subnet`. Requires `subnet`. Auto-assigned when unset.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRoot("subnet")),
				},
			},
			"labels": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Network ID as reported by `nerdctl network inspect`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *networkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := []string{"network", "create", "--driver", plan.Driver.ValueString()}
	if s := plan.Subnet.ValueString(); s != "" {
		args = append(args, "--subnet", s)
	}
	if g := plan.Gateway.ValueString(); g != "" {
		args = append(args, "--gateway", g)
	}
	if !plan.Labels.IsNull() {
		labels := map[string]string{}
		resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &labels, false)...)
		if resp.Diagnostics.HasError() {
			return
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
	args = append(args, plan.Name.ValueString())

	if _, err := r.client.Run(ctx, args...); err != nil {
		resp.Diagnostics.AddError("Failed to create network", err.Error())
		return
	}

	info, err := r.inspect(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect network after create", err.Error())
		return
	}
	plan.ID = types.StringValue(info.ID)
	if len(info.IPAM.Config) > 0 {
		plan.Subnet = types.StringValue(info.IPAM.Config[0].Subnet)
		plan.Gateway = types.StringValue(info.IPAM.Config[0].Gateway)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkResourceModel
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
		resp.Diagnostics.AddError("Failed to inspect network", err.Error())
		return
	}

	state.ID = types.StringValue(info.ID)
	if len(info.IPAM.Config) > 0 {
		state.Subnet = types.StringValue(info.IPAM.Config[0].Subnet)
		state.Gateway = types.StringValue(info.IPAM.Config[0].Gateway)
	}
	// The driver is not present in inspect output; assume the default on
	// import rather than leaving a null that would force a replacement.
	if state.Driver.IsNull() {
		state.Driver = types.StringValue("bridge")
	}

	actual := networkUserLabels(info.Labels)
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
func (r *networkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.Run(ctx, "network", "rm", state.Name.ValueString()); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove network", err.Error())
	}
}

// ImportState imports by network name, e.g.
// `terraform import nerdctl_network.app app-net`. The driver is assumed to
// be `bridge` (see the driver attribute description).
func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *networkResource) inspect(ctx context.Context, name string) (*networkInspect, error) {
	return inspectNetwork(ctx, r.client, name)
}

// inspectNetwork looks up a network, shared by the resource and data source.
func inspectNetwork(ctx context.Context, client *nerdctl.Client, name string) (*networkInspect, error) {
	out, err := client.Run(ctx, "network", "inspect", name)
	if err != nil {
		return nil, err
	}
	return parseNetworkInspect(out)
}

func parseNetworkInspect(out string) (*networkInspect, error) {
	var infos []networkInspect
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parsing network inspect output: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("empty network inspect result")
	}
	return &infos[0], nil
}

// networkUserLabels drops nerdctl bookkeeping (e.g. nerdctl/default-network
// on the default bridge), leaving what the user passed via --label.
func networkUserLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, "nerdctl/") {
			continue
		}
		out[k] = v
	}
	return out
}
