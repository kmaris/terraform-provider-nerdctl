package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*imageLoadAction)(nil)
	_ action.ActionWithConfigure = (*imageLoadAction)(nil)
)

// NewImageLoadAction returns the nerdctl_image_load action, which loads
// images from a tar archive.
func NewImageLoadAction() action.Action { return &imageLoadAction{} }

type imageLoadAction struct {
	client *nerdctl.Client
}

type imageLoadActionModel struct {
	Input types.String `tfsdk:"input"`
}

func (a *imageLoadAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_load"
}

func (a *imageLoadAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Loads images from a tar archive with `nerdctl load`. The archive is read from the target host, not the machine running Terraform.",
		Attributes: map[string]schema.Attribute{
			"input": schema.StringAttribute{
				Required:    true,
				Description: "Path on the target host of the tar archive to load.",
			},
		},
	}
}

func (a *imageLoadAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *imageLoadAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg imageLoadActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := a.client.Run(ctx, "load", "-i", cfg.Input.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to load images", err.Error())
		return
	}
	if out != "" {
		resp.SendProgress(action.InvokeProgressEvent{Message: out})
	}
}
