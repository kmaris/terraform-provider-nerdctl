package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*containerExportAction)(nil)
	_ action.ActionWithConfigure = (*containerExportAction)(nil)
)

func NewContainerExportAction() action.Action { return &containerExportAction{} }

type containerExportAction struct {
	client *nerdctl.Client
}

type containerExportActionModel struct {
	Container types.String `tfsdk:"container"`
	Output    types.String `tfsdk:"output"`
}

func (a *containerExportAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_export"
}

func (a *containerExportAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Exports a container's filesystem as a tar archive with `nerdctl export`. The archive is written on the target host, not the machine running Terraform.",
		Attributes: map[string]schema.Attribute{
			"container": schema.StringAttribute{
				Required:    true,
				Description: "Container name or ID to export.",
			},
			"output": schema.StringAttribute{
				Required:    true,
				Description: "Path on the target host to write the tar archive to.",
			},
		},
	}
}

func (a *containerExportAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *containerExportAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg containerExportActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := []string{"export", "-o", cfg.Output.ValueString(), cfg.Container.ValueString()}
	if _, err := a.client.Run(ctx, args...); err != nil {
		resp.Diagnostics.AddError("Failed to export container", err.Error())
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("exported %s to %s on the target host", cfg.Container.ValueString(), cfg.Output.ValueString()),
	})
}
