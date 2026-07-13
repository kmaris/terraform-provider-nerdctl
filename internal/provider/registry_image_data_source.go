package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ datasource.DataSource              = (*registryImageDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*registryImageDataSource)(nil)
)

// NewRegistryImageDataSource returns the nerdctl_registry_image data source.
func NewRegistryImageDataSource() datasource.DataSource { return &registryImageDataSource{} }

type registryImageDataSource struct {
	client *nerdctl.Client
}

type registryImageDataSourceModel struct {
	Name             types.String `tfsdk:"name"`
	InsecureRegistry types.Bool   `tfsdk:"insecure_registry"`
	SHA256Digest     types.String `tfsdk:"sha256_digest"`
	ID               types.String `tfsdk:"id"`
}

func (d *registryImageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_image"
}

func (d *registryImageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The manifest digest of an image in a remote registry, read with `nerdctl manifest inspect` (requires nerdctl >= 2.3) without pulling. Key a `nerdctl_image`'s `triggers` on `sha256_digest` to re-pull when the remote tag moves.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Image reference to look up, e.g. `alpine:3.20`.",
			},
			"insecure_registry": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow plain-HTTP communication with the registry, passed with `--insecure`.",
			},
			"sha256_digest": schema.StringAttribute{
				Computed:    true,
				Description: "Digest of the remote manifest, e.g. `sha256:...`. For multi-platform references, where the registry check cannot see the index itself, this is a stable digest computed over the per-platform manifest digests.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The image reference.",
			},
		},
	}
}

func (d *registryImageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromDataSourceProviderData(req, resp)
}

func (d *registryImageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg registryImageDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	digest, err := remoteManifestDigest(ctx, d.client, cfg.Name.ValueString(), cfg.InsecureRegistry.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect remote manifest", err.Error())
		return
	}
	cfg.SHA256Digest = types.StringValue(digest)
	cfg.ID = cfg.Name

	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
