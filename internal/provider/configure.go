package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

// clientFromProviderData extracts the shared nerdctl client in each
// resource's Configure. ProviderData is nil during validation passes.
func clientFromProviderData(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *nerdctl.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*nerdctl.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("expected *nerdctl.Client, got: %T", req.ProviderData),
		)
		return nil
	}
	return client
}

// clientFromDataSourceProviderData mirrors clientFromProviderData for data
// sources, which use their own configure request type.
func clientFromDataSourceProviderData(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *nerdctl.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*nerdctl.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected data source configure type",
			fmt.Sprintf("expected *nerdctl.Client, got: %T", req.ProviderData),
		)
		return nil
	}
	return client
}
