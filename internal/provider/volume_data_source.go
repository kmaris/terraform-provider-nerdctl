package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ datasource.DataSource              = (*volumeDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*volumeDataSource)(nil)
)

// NewVolumeDataSource returns the nerdctl_volume data source.
func NewVolumeDataSource() datasource.DataSource { return &volumeDataSource{} }

type volumeDataSource struct {
	client *nerdctl.Client
}

type volumeDataSourceModel struct {
	Name       types.String `tfsdk:"name"`
	Mountpoint types.String `tfsdk:"mountpoint"`
}

func (d *volumeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (d *volumeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing named volume. Fails when absent.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Volume name.",
			},
			"mountpoint": schema.StringAttribute{
				Computed:    true,
				Description: "Directory on the host backing the volume.",
			},
		},
	}
}

func (d *volumeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceProviderData(req, resp)
}

func (d *volumeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg volumeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mountpoint, err := volumeMountpoint(ctx, d.client, cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect volume", err.Error())
		return
	}
	cfg.Mountpoint = types.StringValue(mountpoint)

	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
