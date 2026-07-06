package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ datasource.DataSource              = (*networkDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*networkDataSource)(nil)
)

func NewNetworkDataSource() datasource.DataSource { return &networkDataSource{} }

type networkDataSource struct {
	client *nerdctl.Client
}

type networkDataSourceModel struct {
	Name    types.String `tfsdk:"name"`
	Subnet  types.String `tfsdk:"subnet"`
	Gateway types.String `tfsdk:"gateway"`
	Labels  types.Map    `tfsdk:"labels"`
	ID      types.String `tfsdk:"id"`
}

func (d *networkDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (d *networkDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing CNI network, e.g. the default `bridge`. Fails when absent. The driver is not reported by `network inspect`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
			},
			"subnet": schema.StringAttribute{
				Computed:    true,
				Description: "Subnet in CIDR notation.",
			},
			"gateway": schema.StringAttribute{
				Computed:    true,
				Description: "Gateway address within `subnet`.",
			},
			"labels": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "User labels on the network (nerdctl bookkeeping labels are filtered out).",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Network ID as reported by `nerdctl network inspect`.",
			},
		},
	}
}

func (d *networkDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceProviderData(req, resp)
}

func (d *networkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg networkDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info, err := inspectNetwork(ctx, d.client, cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect network", err.Error())
		return
	}

	cfg.ID = types.StringValue(info.ID)
	cfg.Subnet = types.StringNull()
	cfg.Gateway = types.StringNull()
	if len(info.IPAM.Config) > 0 {
		cfg.Subnet = types.StringValue(info.IPAM.Config[0].Subnet)
		cfg.Gateway = types.StringValue(info.IPAM.Config[0].Gateway)
	}
	labels, diags := types.MapValueFrom(ctx, types.StringType, networkUserLabels(info.Labels))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg.Labels = labels

	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
