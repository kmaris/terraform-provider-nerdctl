package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ datasource.DataSource              = (*containerDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*containerDataSource)(nil)
)

// NewContainerDataSource returns the nerdctl_container data source.
func NewContainerDataSource() datasource.DataSource { return &containerDataSource{} }

type containerDataSource struct {
	client *nerdctl.Client
}

type containerDataSourceModel struct {
	Name       types.String  `tfsdk:"name"`
	ID         types.String  `tfsdk:"id"`
	Image      types.String  `tfsdk:"image"`
	Status     types.String  `tfsdk:"status"`
	Running    types.Bool    `tfsdk:"running"`
	Pid        types.Int64   `tfsdk:"pid"`
	Restart    types.String  `tfsdk:"restart"`
	Memory     types.Int64   `tfsdk:"memory"`
	Cpus       types.Float64 `tfsdk:"cpus"`
	Privileged types.Bool    `tfsdk:"privileged"`
	Networks   types.List    `tfsdk:"networks"`
	Labels     types.Map     `tfsdk:"labels"`
	Env        types.Map     `tfsdk:"env"`
	Ports      types.List    `tfsdk:"ports"`
}

func (d *containerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (d *containerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing container, read via `nerdctl container inspect`. Fails when absent.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Container name.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Container ID.",
			},
			"image": schema.StringAttribute{
				Computed:    true,
				Description: "Image the container runs, with the implied `docker.io` registry prefixes stripped.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Container state: `created`, `running`, `paused`, `restarting`, `removing`, `exited`, or `dead`.",
			},
			"running": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the container is currently running.",
			},
			"pid": schema.Int64Attribute{
				Computed:    true,
				Description: "Host PID of the container's main process, or `0` when not running.",
			},
			"restart": schema.StringAttribute{
				Computed:    true,
				Description: "Restart policy: `no`, `always`, `unless-stopped`, or `on-failure[:max-retries]`.",
			},
			"memory": schema.Int64Attribute{
				Computed:    true,
				Description: "Memory limit in bytes, or `0` when unlimited.",
			},
			"cpus": schema.Float64Attribute{
				Computed:    true,
				Description: "CPU limit in cores derived from the cgroup quota and period, or `0` when unlimited.",
			},
			"privileged": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the container runs with extended privileges.",
			},
			"networks": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Networks the container is attached to, in interface order.",
			},
			"labels": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Labels on the container (nerdctl and containerd bookkeeping labels are filtered out).",
			},
			"env": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
				Description: "Every environment variable in the container's spec, including image and runtime defaults.",
			},
			"ports": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Published ports.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"internal": schema.Int64Attribute{
							Computed:    true,
							Description: "Port inside the container.",
						},
						"external": schema.Int64Attribute{
							Computed:    true,
							Description: "Port published on the host.",
						},
						"protocol": schema.StringAttribute{
							Computed:    true,
							Description: "`tcp`, `udp`, or `sctp`.",
						},
					},
				},
			},
		},
	}
}

func (d *containerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceProviderData(req, resp)
}

func (d *containerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg containerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := d.client.Run(ctx, "container", "inspect", cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect container", err.Error())
		return
	}
	info, err := parseContainerInspect(out)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse container inspect output", err.Error())
		return
	}

	cfg.ID = types.StringValue(info.ID)
	cfg.Image = types.StringValue(normalizeImageRef(info.Image))
	cfg.Status = types.StringValue(info.State.Status)
	cfg.Running = types.BoolValue(info.State.Running)
	cfg.Pid = types.Int64Value(int64(info.State.Pid))
	cfg.Restart = types.StringValue(info.restartPolicy())
	cfg.Memory = types.Int64Value(info.HostConfig.Memory)
	cfg.Cpus = types.Float64Value(info.cpus())
	cfg.Privileged = types.BoolValue(info.HostConfig.Privileged)

	networks, diags := types.ListValueFrom(ctx, types.StringType, info.networks())
	resp.Diagnostics.Append(diags...)
	cfg.Networks = networks

	labels, diags := types.MapValueFrom(ctx, types.StringType, info.userLabels(nil))
	resp.Diagnostics.Append(diags...)
	cfg.Labels = labels

	env, diags := types.MapValueFrom(ctx, types.StringType, info.envMap())
	resp.Diagnostics.Append(diags...)
	cfg.Env = env

	ports, err := info.portModels()
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse container ports", err.Error())
		return
	}
	portList, diags := types.ListValueFrom(ctx, portObjectType, ports)
	resp.Diagnostics.Append(diags...)
	cfg.Ports = portList

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
