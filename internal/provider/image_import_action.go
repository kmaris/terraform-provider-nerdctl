package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*imageImportAction)(nil)
	_ action.ActionWithConfigure = (*imageImportAction)(nil)
)

// NewImageImportAction returns the nerdctl_image_import action, which
// creates an image from a filesystem tar archive.
func NewImageImportAction() action.Action { return &imageImportAction{} }

type imageImportAction struct {
	client *nerdctl.Client
}

type imageImportActionModel struct {
	Input     types.String `tfsdk:"input"`
	Reference types.String `tfsdk:"reference"`
}

func (a *imageImportAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_import"
}

func (a *imageImportAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Creates a filesystem image from a tarball with `nerdctl import`. The tarball path or URL is resolved on the target host, not the machine running Terraform.",
		Attributes: map[string]schema.Attribute{
			"input": schema.StringAttribute{
				Required:    true,
				Description: "Tarball to import: a path on the target host, a URL, or `-` is not supported (no stdin).",
			},
			"reference": schema.StringAttribute{
				Optional:    true,
				Description: "Image reference to tag the result with, `repository[:tag]`.",
			},
		},
	}
}

func (a *imageImportAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *imageImportAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg imageImportActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := []string{"import", cfg.Input.ValueString()}
	if ref := cfg.Reference.ValueString(); ref != "" {
		args = append(args, ref)
	}
	out, err := a.client.Run(ctx, args...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import image", err.Error())
		return
	}
	if out != "" {
		resp.SendProgress(action.InvokeProgressEvent{Message: out})
	}
}
