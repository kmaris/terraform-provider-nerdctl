package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*imageSaveAction)(nil)
	_ action.ActionWithConfigure = (*imageSaveAction)(nil)
)

func NewImageSaveAction() action.Action { return &imageSaveAction{} }

type imageSaveAction struct {
	client *nerdctl.Client
}

type imageSaveActionModel struct {
	Images types.List   `tfsdk:"images"`
	Output types.String `tfsdk:"output"`
}

func (a *imageSaveAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_save"
}

func (a *imageSaveAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Saves images to a tar archive with `nerdctl save`. The archive is written on the target host, not the machine running Terraform.",
		Attributes: map[string]schema.Attribute{
			"images": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Image references to save.",
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"output": schema.StringAttribute{
				Required:    true,
				Description: "Path on the target host to write the tar archive to.",
			},
		},
	}
}

func (a *imageSaveAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *imageSaveAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg imageSaveActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var images []string
	resp.Diagnostics.Append(cfg.Images.ElementsAs(ctx, &images, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args := append([]string{"save", "-o", cfg.Output.ValueString()}, images...)
	if _, err := a.client.Run(ctx, args...); err != nil {
		resp.Diagnostics.AddError("Failed to save images", err.Error())
		return
	}
	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("saved %s to %s on the target host", strings.Join(images, ", "), cfg.Output.ValueString()),
	})
}
