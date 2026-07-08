package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	_ action.Action              = (*execAction)(nil)
	_ action.ActionWithConfigure = (*execAction)(nil)
)

func NewExecAction() action.Action { return &execAction{} }

type execAction struct {
	client *nerdctl.Client
}

type execActionModel struct {
	Container  types.String `tfsdk:"container"`
	Command    types.List   `tfsdk:"command"`
	Env        types.List   `tfsdk:"env"`
	User       types.String `tfsdk:"user"`
	Workdir    types.String `tfsdk:"workdir"`
	Privileged types.Bool   `tfsdk:"privileged"`
	Detach     types.Bool   `tfsdk:"detach"`
}

func (a *execAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_exec"
}

func (a *execAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Runs a command in an existing container with `nerdctl exec`. Output is emitted as action progress; it cannot be captured into state. No TTY is allocated.",
		Attributes: map[string]schema.Attribute{
			"container": schema.StringAttribute{
				Required:    true,
				Description: "Container name or ID to execute in.",
			},
			"command": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "Command and arguments to execute.",
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
			"env": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Environment variables for the command, as `KEY=value` entries.",
			},
			"user": schema.StringAttribute{
				Optional:    true,
				Description: "User to run as, `user[:group]` by name or ID.",
			},
			"workdir": schema.StringAttribute{
				Optional:    true,
				Description: "Working directory inside the container.",
			},
			"privileged": schema.BoolAttribute{
				Optional:    true,
				Description: "Give extended privileges to the command.",
			},
			"detach": schema.BoolAttribute{
				Optional:    true,
				Description: "Run the command in the background and return immediately.",
			},
		},
	}
}

func (a *execAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromActionProviderData(req, resp)
}

func (a *execAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg execActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var command, env []string
	resp.Diagnostics.Append(cfg.Command.ElementsAs(ctx, &command, false)...)
	if !cfg.Env.IsNull() {
		resp.Diagnostics.Append(cfg.Env.ElementsAs(ctx, &env, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	args := execArgs(cfg.Container.ValueString(), command, env,
		cfg.User.ValueString(), cfg.Workdir.ValueString(),
		cfg.Privileged.ValueBool(), cfg.Detach.ValueBool())

	out, err := a.client.Run(ctx, args...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to exec in container", err.Error())
		return
	}
	if out != "" {
		resp.SendProgress(action.InvokeProgressEvent{Message: out})
	}
}

func execArgs(container string, command, env []string, user, workdir string, privileged, detach bool) []string {
	args := []string{"exec"}
	if detach {
		args = append(args, "-d")
	}
	if privileged {
		args = append(args, "--privileged")
	}
	if user != "" {
		args = append(args, "--user", user)
	}
	if workdir != "" {
		args = append(args, "--workdir", workdir)
	}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, container)
	return append(args, command...)
}
