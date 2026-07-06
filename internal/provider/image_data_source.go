package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ datasource.DataSource              = (*imageDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*imageDataSource)(nil)
)

func NewImageDataSource() datasource.DataSource { return &imageDataSource{} }

type imageDataSource struct {
	client *nerdctl.Client
}

type imageDataSourceModel struct {
	Name types.String `tfsdk:"name"`
	ID   types.String `tfsdk:"id"`
}

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An image already present on the host. Fails when absent; use the `nerdctl_image` resource to pull.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Image reference, e.g. `traefik:v3`.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Image ID (digest) as reported by `nerdctl image inspect`.",
			},
		},
	}
}

func (d *imageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceProviderData(req, resp)
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg imageDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := imageID(ctx, d.client, cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect image", err.Error())
		return
	}
	cfg.ID = types.StringValue(id)

	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
