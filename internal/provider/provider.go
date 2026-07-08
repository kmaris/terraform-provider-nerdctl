package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ provider.Provider            = (*nerdctlProvider)(nil)
	_ provider.ProviderWithActions = (*nerdctlProvider)(nil)
)

type nerdctlProvider struct {
	version string
}

type nerdctlProviderModel struct {
	Host        types.String `tfsdk:"host"`
	SSHOpts     types.List   `tfsdk:"ssh_opts"`
	NerdctlPath types.String `tfsdk:"nerdctl_path"`
	Namespace   types.String `tfsdk:"namespace"`
	Sudo        types.Bool   `tfsdk:"sudo"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &nerdctlProvider{version: version}
	}
}

func (p *nerdctlProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "nerdctl"
	resp.Version = p.version
}

func (p *nerdctlProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage containers with nerdctl/containerd, locally or over ssh.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional:    true,
				Description: "Remote host to run nerdctl on, as `ssh://[user@]host[:port]`. Requires non-interactive ssh (key auth). Runs nerdctl locally when unset.",
			},
			"ssh_opts": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Extra ssh CLI options for remote hosts, e.g. `[\"-i\", \"~/.ssh/deploy_key\", \"-J\", \"bastion\"]`. Same shape as the docker provider's `ssh_opts`.",
			},
			"nerdctl_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to the nerdctl binary on the target host. Defaults to `nerdctl`.",
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Description: "containerd namespace to operate in. Defaults to `default`.",
			},
			"sudo": schema.BoolAttribute{
				Optional:    true,
				Description: "Run nerdctl under `sudo -n` (requires passwordless sudo). Needed for rootful containerd when connecting as a non-root user.",
			},
		},
	}
}

func (p *nerdctlProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg nerdctlProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var sshOpts []string
	if !cfg.SSHOpts.IsNull() && !cfg.SSHOpts.IsUnknown() {
		resp.Diagnostics.Append(cfg.SSHOpts.ElementsAs(ctx, &sshOpts, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	client, err := nerdctl.New(nerdctl.Config{
		Host:        cfg.Host.ValueString(),
		SSHOpts:     sshOpts,
		NerdctlPath: cfg.NerdctlPath.ValueString(),
		Namespace:   cfg.Namespace.ValueString(),
		Sudo:        cfg.Sudo.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}

	resp.ResourceData = client
	resp.DataSourceData = client
	resp.ActionData = client
}

func (p *nerdctlProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewImageResource,
		NewVolumeResource,
		NewNetworkResource,
		NewContainerResource,
	}
}

func (p *nerdctlProvider) Actions(context.Context) []func() action.Action {
	return []func() action.Action{
		NewExecAction,
		NewContainerExportAction,
		NewImageImportAction,
		NewImageLoadAction,
		NewImageSaveAction,
		NewSystemPruneAction,
	}
}

func (p *nerdctlProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewImageDataSource,
		NewVolumeDataSource,
		NewNetworkDataSource,
	}
}
