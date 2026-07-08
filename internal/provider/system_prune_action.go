package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*systemPruneAction)(nil)
	_ action.ActionWithConfigure = (*systemPruneAction)(nil)
)

func NewSystemPruneAction() action.Action { return &systemPruneAction{} }

type systemPruneAction struct {
	client *nerdctl.Client
}

type systemPruneActionModel struct {
	All     types.Bool `tfsdk:"all"`
	Volumes types.Bool `tfsdk:"volumes"`
}

func (a *systemPruneAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_prune"
}

func (a *systemPruneAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Removes unused data with `nerdctl system prune --force`. Destructive: it also removes objects not managed by Terraform (stopped containers, unused networks and images, and with `volumes`, unused volumes).",
		Attributes: map[string]schema.Attribute{
			"all": schema.BoolAttribute{
				Optional:    true,
				Description: "Remove all unused images, not just dangling ones.",
			},
			"volumes": schema.BoolAttribute{
				Optional:    true,
				Description: "Also prune unused volumes.",
			},
		},
	}
}

func (a *systemPruneAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *systemPruneAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg systemPruneActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := systemPruneArgs(cfg.All.ValueBool(), cfg.Volumes.ValueBool())
	out, err := a.client.Run(ctx, args...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to prune system", err.Error())
		return
	}
	if out != "" {
		resp.SendProgress(action.InvokeProgressEvent{Message: out})
	}
}

func systemPruneArgs(all, volumes bool) []string {
	args := []string{"system", "prune", "--force"}
	if all {
		args = append(args, "--all")
	}
	if volumes {
		args = append(args, "--volumes")
	}
	return args
}
