package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*containerStateAction)(nil)
	_ action.ActionWithConfigure = (*containerStateAction)(nil)
)

// NewContainerStartAction returns the nerdctl_container_start action.
func NewContainerStartAction() action.Action {
	return &containerStateAction{verb: "start", description: "Starts a stopped container with `nerdctl start`."}
}

// NewContainerStopAction returns the nerdctl_container_stop action.
func NewContainerStopAction() action.Action {
	return &containerStateAction{verb: "stop", description: "Stops a running container with `nerdctl stop`."}
}

// NewContainerRestartAction returns the nerdctl_container_restart action.
func NewContainerRestartAction() action.Action {
	return &containerStateAction{verb: "restart", description: "Restarts a container with `nerdctl restart`."}
}

// containerStateAction drives a container state transition; start, stop, and
// restart differ only in the nerdctl verb. The timeout and signal attributes
// are declared for all three but only apply to stop and restart, which is
// enforced at plan time.
type containerStateAction struct {
	client      *nerdctl.Client
	verb        string
	description string
}

type containerStateActionModel struct {
	Container types.String `tfsdk:"container"`
	Timeout   types.Int64  `tfsdk:"timeout"`
	Signal    types.String `tfsdk:"signal"`
}

func (a *containerStateAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_" + a.verb
}

func (a *containerStateAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"container": schema.StringAttribute{
			Required:    true,
			Description: "Container name or ID.",
		},
	}
	if a.verb != "start" {
		attrs["timeout"] = schema.Int64Attribute{
			Optional:    true,
			Description: "Seconds to wait after the stop signal before killing the container, passed with `-t`. Defaults to 10.",
			Validators:  []validator.Int64{int64validator.AtLeast(0)},
		}
		attrs["signal"] = schema.StringAttribute{
			Optional:    true,
			Description: "Signal to send, e.g. `SIGINT`, passed with `-s`. Defaults to the container's stop signal.",
		}
	}
	resp.Schema = schema.Schema{
		Description: a.description,
		Attributes:  attrs,
	}
}

func (a *containerStateAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *containerStateAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	cfg := containerStateActionModel{Timeout: types.Int64Null(), Signal: types.StringNull()}
	if a.verb == "start" {
		var startCfg struct {
			Container types.String `tfsdk:"container"`
		}
		resp.Diagnostics.Append(req.Config.Get(ctx, &startCfg)...)
		cfg.Container = startCfg.Container
	} else {
		resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := a.client.Run(ctx, containerStateArgs(a.verb, &cfg)...); err != nil {
		resp.Diagnostics.AddError("Failed to "+a.verb+" container", err.Error())
	}
}

// containerStateArgs builds the start/stop/restart argument list.
func containerStateArgs(verb string, m *containerStateActionModel) []string {
	args := []string{verb}
	if !m.Timeout.IsNull() {
		args = append(args, "-t", strconv.FormatInt(m.Timeout.ValueInt64(), 10))
	}
	if s := m.Signal.ValueString(); s != "" {
		args = append(args, "-s", s)
	}
	return append(args, m.Container.ValueString())
}
