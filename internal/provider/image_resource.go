package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ resource.Resource                = (*imageResource)(nil)
	_ resource.ResourceWithConfigure   = (*imageResource)(nil)
	_ resource.ResourceWithImportState = (*imageResource)(nil)
)

func NewImageResource() resource.Resource { return &imageResource{} }

type imageResource struct {
	client *nerdctl.Client
}

type imageResourceModel struct {
	Name types.String `tfsdk:"name"`
	ID   types.String `tfsdk:"id"`
}

func (r *imageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (r *imageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An image pulled with nerdctl.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Image reference to pull, e.g. `traefik:v3`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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

	if _, err := r.client.Run(ctx, "pull", "--quiet", plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to pull image", err.Error())
		return
	}

	id, err := r.imageID(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect image after pull", err.Error())
		return
	}
	plan.ID = types.StringValue(id)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.imageID(ctx, state.Name.ValueString())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect image", err.Error())
		return
	}
	state.ID = types.StringValue(id)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: every attribute requires replacement.
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

	if _, err := r.client.Run(ctx, "rmi", state.Name.ValueString()); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove image", err.Error())
	}
}

// ImportState imports by image reference, e.g.
// `terraform import nerdctl_image.traefik traefik:v3`.
func (r *imageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *imageResource) imageID(ctx context.Context, name string) (string, error) {
	out, err := r.client.Run(ctx, "image", "inspect", name)
	if err != nil {
		return "", err
	}
	var infos []struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return "", fmt.Errorf("parsing image inspect output: %w", err)
	}
	if len(infos) == 0 {
		return "", fmt.Errorf("image %s: empty inspect result", name)
	}
	return infos[0].ID, nil
}
