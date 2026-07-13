package provider

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/kmaris/terraform-provider-nerdctl/internal/nerdctl"
)

var (
	restartPolicyRe = regexp.MustCompile(`^(no|always|unless-stopped|on-failure(:\d+)?)$`)
	memorySizeRe    = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[bkmgtBKMGT]?[bB]?$`)
)

var (
	_ resource.Resource                = (*containerResource)(nil)
	_ resource.ResourceWithConfigure   = (*containerResource)(nil)
	_ resource.ResourceWithImportState = (*containerResource)(nil)
)

var (
	portObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"internal": types.Int64Type,
		"external": types.Int64Type,
		"protocol": types.StringType,
	}}
	volumeObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"container_path": types.StringType,
		"host_path":      types.StringType,
		"volume_name":    types.StringType,
		"read_only":      types.BoolType,
	}}
	healthcheckObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"command":      types.StringType,
		"interval":     types.StringType,
		"timeout":      types.StringType,
		"start_period": types.StringType,
		"retries":      types.Int64Type,
	}}
)

// NewContainerResource returns the nerdctl_container resource.
func NewContainerResource() resource.Resource { return &containerResource{} }

type containerResource struct {
	client *nerdctl.Client
}

type containerResourceModel struct {
	Name          types.String  `tfsdk:"name"`
	Image         types.String  `tfsdk:"image"`
	Command       types.List    `tfsdk:"command"`
	Entrypoint    types.String  `tfsdk:"entrypoint"`
	Restart       types.String  `tfsdk:"restart"`
	User          types.String  `tfsdk:"user"`
	Workdir       types.String  `tfsdk:"workdir"`
	Hostname      types.String  `tfsdk:"hostname"`
	Memory        types.String  `tfsdk:"memory"`
	Cpus          types.Float64 `tfsdk:"cpus"`
	Privileged    types.Bool    `tfsdk:"privileged"`
	CapAdd        types.List    `tfsdk:"cap_add"`
	CapDrop       types.List    `tfsdk:"cap_drop"`
	Sysctls       types.Map     `tfsdk:"sysctls"`
	Tmpfs         types.Map     `tfsdk:"tmpfs"`
	LogDriver     types.String  `tfsdk:"log_driver"`
	LogOpts       types.Map     `tfsdk:"log_opts"`
	Healthcheck   types.Object  `tfsdk:"healthcheck"`
	NoHealthcheck types.Bool    `tfsdk:"no_healthcheck"`
	Networks      types.List    `tfsdk:"networks"`
	IP            types.String  `tfsdk:"ip"`
	IP6           types.String  `tfsdk:"ip6"`
	MacAddress    types.String  `tfsdk:"mac_address"`
	ExtraHosts    types.Map     `tfsdk:"extra_hosts"`
	DNS           types.List    `tfsdk:"dns"`
	DNSOpts       types.List    `tfsdk:"dns_opts"`
	DNSSearch     types.List    `tfsdk:"dns_search"`
	Env           types.Map     `tfsdk:"env"`
	Ports         types.List    `tfsdk:"ports"`
	Labels        types.Map     `tfsdk:"labels"`
	Volumes       types.List    `tfsdk:"volumes"`
	ID            types.String  `tfsdk:"id"`
}

type healthcheckModel struct {
	Command     types.String `tfsdk:"command"`
	Interval    types.String `tfsdk:"interval"`
	Timeout     types.String `tfsdk:"timeout"`
	StartPeriod types.String `tfsdk:"start_period"`
	Retries     types.Int64  `tfsdk:"retries"`
}

type portModel struct {
	Internal types.Int64  `tfsdk:"internal"`
	External types.Int64  `tfsdk:"external"`
	Protocol types.String `tfsdk:"protocol"`
}

type volumeMountModel struct {
	ContainerPath types.String `tfsdk:"container_path"`
	HostPath      types.String `tfsdk:"host_path"`
	VolumeName    types.String `tfsdk:"volume_name"`
	ReadOnly      types.Bool   `tfsdk:"read_only"`
}

func (r *containerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (r *containerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A container run with `nerdctl run -d`. Containers are treated as immutable: every change forces a replacement.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Container name, unique on the host.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"image": schema.StringAttribute{
				Required:      true,
				Description:   "Image reference. Reference a `nerdctl_image` name to order the pull first.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"command": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Command and arguments passed after the image.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"entrypoint": schema.StringAttribute{
				Optional:      true,
				Description:   "Overrides the image entrypoint binary. As with `command`, drift is not detected.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"user": schema.StringAttribute{
				Optional:      true,
				Description:   "User to run as, `user[:group]` by name or ID. When unset, the image default applies and drift is not detected.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"workdir": schema.StringAttribute{
				Optional:      true,
				Description:   "Working directory inside the container. Drift is not detected (absent from inspect output).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				Optional:      true,
				Description:   "Container hostname. When unset, the runtime default applies and drift is not detected.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"memory": schema.StringAttribute{
				Optional:      true,
				Description:   "Memory limit as a docker-style size, e.g. `512m` or `2g`. Rootless hosts need cgroup v2 delegation.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(memorySizeRe, "must be a size like 512m, 2g, or 1073741824"),
				},
			},
			"cpus": schema.Float64Attribute{
				Optional:      true,
				Description:   "CPU limit in cores, e.g. `1.5`. Rootless hosts need cgroup v2 delegation.",
				PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()},
				Validators:    []validator.Float64{float64validator.AtLeast(0.01)},
			},
			"restart": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("unless-stopped"),
				Description:   "Restart policy handled by containerd's restart manager: `no`, `always`, `unless-stopped`, or `on-failure[:max-retries]`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(restartPolicyRe, "must be no, always, unless-stopped, or on-failure[:max-retries]"),
				},
			},
			"privileged": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				Description:   "Run with extended privileges (`--privileged`). Capabilities are not tracked on privileged containers, which hold all of them.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"cap_add": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Linux capabilities to add, without the `CAP_` prefix, e.g. `NET_ADMIN`. `all` is rejected: inspect output reconstructs capabilities individually, so it cannot round-trip.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.NoneOfCaseInsensitive("all")),
				},
			},
			"cap_drop": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Linux capabilities to drop, without the `CAP_` prefix, e.g. `MKNOD`. `all` is rejected for the same reason as in `cap_add`.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.NoneOfCaseInsensitive("all")),
				},
			},
			"sysctls": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Namespaced kernel parameters set with `--sysctl`, e.g. `net.core.somaxconn`.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"tmpfs": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "tmpfs mounts keyed by container path, with mount options as the value (empty for defaults), e.g. `{\"/run\" = \"size=64m\"}`. nerdctl always adds `noexec,nosuid,nodev` unless overridden; the comparison accounts for that.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"log_driver": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("json-file"),
				Description:   "Logging driver passed with `--log-driver`. `none` is not offered: inspect output cannot distinguish it from the default, so it would drift on every plan.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf("json-file", "journald", "fluentd", "syslog"),
				},
			},
			"log_opts": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Driver-specific logging options passed with `--log-opt`, e.g. `max-size`.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"healthcheck": schema.SingleNestedAttribute{
				Optional:      true,
				Description:   "Health check run inside the container. Requires nerdctl >= 2.1.5. When unset, the image healthcheck (if any) applies and drift is not detected; the same applies after `terraform import`.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"command": schema.StringAttribute{
						Required:    true,
						Description: "Shell command passed with `--health-cmd`, run as `CMD-SHELL`.",
					},
					"interval": schema.StringAttribute{
						Optional:    true,
						Description: "Time between checks as a Go duration, e.g. `30s` (the default).",
						Validators:  []validator.String{durationString{}},
					},
					"timeout": schema.StringAttribute{
						Optional:    true,
						Description: "Time before a check counts as failed, e.g. `30s` (the default).",
						Validators:  []validator.String{durationString{}},
					},
					"start_period": schema.StringAttribute{
						Optional:    true,
						Description: "Startup grace period during which failures don't count, e.g. `5s`. Defaults to `0s`.",
						Validators:  []validator.String{durationString{allowZero: true}},
					},
					"retries": schema.Int64Attribute{
						Optional:    true,
						Description: "Consecutive failures needed to mark the container unhealthy. Defaults to 3.",
						Validators:  []validator.Int64{int64validator.AtLeast(1)},
					},
				},
			},
			"no_healthcheck": schema.BoolAttribute{
				Optional:      true,
				Description:   "Disable any healthcheck defined by the image, passed with `--no-healthcheck`. Conflicts with `healthcheck`.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
				Validators: []validator.Bool{
					boolvalidator.ConflictsWith(path.MatchRoot("healthcheck")),
				},
			},
			"networks": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Networks to attach, e.g. `nerdctl_network` names. Runs on the default bridge when unset. Containers on the same named network resolve each other by container name (nerdctl has no `--network-alias`).",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"ip": schema.StringAttribute{
				Optional:      true,
				Description:   "Static IPv4 address, passed with `--ip`. The network must have a known subnet, e.g. a `nerdctl_network` with `subnet` set; unlike docker, nerdctl also allows this on the default bridge.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{ipString{}},
			},
			"ip6": schema.StringAttribute{
				Optional:      true,
				Description:   "Static IPv6 address, passed with `--ip6`. Requires an IPv6-enabled network.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{ipString{v6: true}},
			},
			"mac_address": schema.StringAttribute{
				Optional:      true,
				Description:   "Container MAC address, passed with `--mac-address`. Supported on `bridge` and `macvlan` networks; uniqueness is not checked.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{macString{}},
			},
			"extra_hosts": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Extra `/etc/hosts` entries keyed by hostname, each passed as `--add-host host:ip`. The special value `host-gateway` resolves to the host's gateway address.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
				Validators:    []validator.Map{mapvalidator.ValueStringsAre(hostIPValue{})},
			},
			"dns": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "DNS nameservers written to the container's resolv.conf, passed with `--dns`. Inherits the host's resolver configuration when unset.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"dns_opts": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "resolv.conf options like `ndots:2`, passed with `--dns-option`.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"dns_search": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "DNS search domains for short-name lookups, passed with `--dns-search`.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"env": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Environment variables passed with `-e`. Variables the image already defines with the same value are treated as image-provided, not managed.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"labels": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Description:   "Labels applied with `--label`.",
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"ports": schema.ListNestedAttribute{
				Optional:      true,
				Description:   "Ports published with `-p`. Rootless hosts cannot bind external ports below 1024.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"internal": schema.Int64Attribute{
							Required:    true,
							Description: "Port inside the container.",
							Validators:  []validator.Int64{int64validator.Between(1, 65535)},
						},
						"external": schema.Int64Attribute{
							Required:    true,
							Description: "Port published on the host.",
							Validators:  []validator.Int64{int64validator.Between(1, 65535)},
						},
						"protocol": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Description: "`tcp` (default), `udp`, or `sctp`.",
							Default:     stringdefault.StaticString("tcp"),
							Validators:  []validator.String{stringvalidator.OneOf("tcp", "udp", "sctp")},
						},
					},
				},
			},
			"volumes": schema.ListNestedAttribute{
				Optional:      true,
				Description:   "Mounts. Set `host_path` for a bind mount or `volume_name` for a named volume, not both.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"container_path": schema.StringAttribute{
							Required:    true,
							Description: "Mount destination inside the container.",
						},
						"host_path": schema.StringAttribute{
							Optional:    true,
							Description: "Host directory for a bind mount.",
							Validators: []validator.String{
								stringvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("volume_name")),
							},
						},
						"volume_name": schema.StringAttribute{
							Optional:    true,
							Description: "Named volume to mount, e.g. a `nerdctl_volume` name.",
						},
						"read_only": schema.BoolAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Mount read-only. Defaults to `false`.",
							Default:     booldefault.StaticBool(false),
						},
					},
				},
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Container ID as reported by `nerdctl container inspect`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// durationString validates that a string parses as a Go duration. nerdctl
// parses the healthcheck flags the same way, so validation here means bad
// values fail at plan time instead of mid-apply.
type durationString struct {
	allowZero bool
}

func (durationString) Description(context.Context) string {
	return "a Go duration like \"30s\" or \"1m30s\""
}

func (v durationString) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v durationString) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	d, err := time.ParseDuration(req.ConfigValue.ValueString())
	if err != nil || d < 0 || (d == 0 && !v.allowZero) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid duration",
			fmt.Sprintf("%q must be a positive Go duration like \"30s\" or \"1m30s\"", req.ConfigValue.ValueString()))
	}
}

// ipString validates an IP literal of the right family at plan time, the
// same check nerdctl applies when parsing --ip/--ip6.
type ipString struct {
	v6 bool
}

func (v ipString) Description(context.Context) string {
	if v.v6 {
		return "an IPv6 address like \"fd00::5\""
	}
	return "an IPv4 address like \"10.4.0.5\""
}

func (v ipString) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ipString) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	addr, err := netip.ParseAddr(req.ConfigValue.ValueString())
	if err != nil || addr.Is4() == v.v6 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid IP address",
			fmt.Sprintf("%q must be %s", req.ConfigValue.ValueString(), v.Description(ctx)))
	}
}

// macString validates a MAC address at plan time.
type macString struct{}

func (macString) Description(context.Context) string {
	return "a MAC address like \"02:ac:ce:55:00:01\""
}

func (m macString) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m macString) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := net.ParseMAC(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid MAC address",
			fmt.Sprintf("%q must be %s", req.ConfigValue.ValueString(), m.Description(ctx)))
	}
}

// hostIPValue validates extra_hosts values: an IP literal or the special
// host-gateway alias nerdctl resolves to the host's gateway address.
type hostIPValue struct{}

func (hostIPValue) Description(context.Context) string {
	return "an IP address or \"host-gateway\""
}

func (h hostIPValue) MarkdownDescription(ctx context.Context) string {
	return h.Description(ctx)
}

func (h hostIPValue) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if v == "host-gateway" {
		return
	}
	if _, err := netip.ParseAddr(v); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid extra_hosts value",
			fmt.Sprintf("%q must be %s", v, h.Description(ctx)))
	}
}

func (r *containerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req, resp)
}

func (r *containerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	args, diags := buildRunArgs(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.Run(ctx, args...)
	if err != nil {
		resp.Diagnostics.AddError("Failed to run container", err.Error())
		return
	}
	plan.ID = types.StringValue(id)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.Run(ctx, "container", "inspect", state.Name.ValueString())
	if nerdctl.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to inspect container", err.Error())
		return
	}

	info, err := parseContainerInspect(out)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse container inspect output", err.Error())
		return
	}

	state.ID = types.StringValue(info.ID)

	if normalizeImageRef(info.Image) != normalizeImageRef(state.Image.ValueString()) {
		state.Image = types.StringValue(normalizeImageRef(info.Image))
	}

	if policy := info.restartPolicy(); policy != state.Restart.ValueString() {
		state.Restart = types.StringValue(policy)
	}

	// command and entrypoint are deliberately left untouched: the OCI spec
	// merges them, so neither is recoverable from inspect output. workdir is
	// absent from inspect output entirely.

	// user and hostname reflect image/runtime defaults when unset, which
	// are indistinguishable from user config; track drift only when the
	// config manages them.
	refreshManagedString(&state.User, info.Config.User)
	refreshManagedString(&state.Hostname, info.Config.Hostname)

	resp.Diagnostics.Append(refreshMemory(&state, info)...)
	refreshCpus(&state, info)
	refreshPrivileged(&state, info)
	resp.Diagnostics.Append(refreshCaps(ctx, &state.CapAdd, info.HostConfig.CapAdd, info.HostConfig.Privileged)...)
	resp.Diagnostics.Append(refreshCaps(ctx, &state.CapDrop, info.HostConfig.CapDrop, info.HostConfig.Privileged)...)
	resp.Diagnostics.Append(refreshSysctls(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshTmpfs(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshLogConfig(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshHealthcheck(ctx, &state, info)...)

	// Image labels and env merge into the container's; fetch them so they
	// can be subtracted. Best-effort: without them (image removed
	// out-of-band), image-defined entries would surface as drift.
	imageLabels := map[string]string{}
	var imageEnv []string
	if imgOut, err := r.client.Run(ctx, "image", "inspect", info.Image); err == nil {
		if parsed, err := parseImageInspect(imgOut); err == nil {
			imageLabels = parsed.Config.Labels
			imageEnv = parsed.Config.Env
		}
	}

	resp.Diagnostics.Append(refreshLabels(ctx, &state, info, imageLabels)...)
	resp.Diagnostics.Append(refreshEnv(ctx, &state, info, imageEnv)...)
	resp.Diagnostics.Append(refreshNetworks(ctx, &state, info)...)
	refreshLabelString(&state.IP, info.staticIP())
	refreshLabelString(&state.IP6, info.staticIP6())
	refreshLabelString(&state.MacAddress, info.macAddress())
	resp.Diagnostics.Append(refreshExtraHosts(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshStringList(ctx, &state.DNS, info.HostConfig.DNS)...)
	resp.Diagnostics.Append(refreshStringList(ctx, &state.DNSOpts, info.HostConfig.DNSOptions)...)
	resp.Diagnostics.Append(refreshStringList(ctx, &state.DNSSearch, info.HostConfig.DNSSearch)...)
	resp.Diagnostics.Append(refreshPorts(ctx, &state, info)...)
	resp.Diagnostics.Append(refreshVolumes(ctx, &state, info)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// refreshLabels overwrites state labels when the container's user labels
// differ. State is kept as-is on a semantic match so null vs empty and
// map ordering never show as drift.
func refreshLabels(ctx context.Context, state *containerResourceModel, info *containerInspect, imageLabels map[string]string) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.userLabels(imageLabels)

	current := map[string]string{}
	if !state.Labels.IsNull() {
		diags.Append(state.Labels.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Labels = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Labels = m
	return diags
}

// refreshManagedString updates a string attribute from the inspected value,
// but only when the config manages it — an unset attribute means the
// image/runtime default applies, which inspect output cannot distinguish
// from explicit configuration.
func refreshManagedString(state *types.String, actual string) {
	if state.IsNull() || actual == state.ValueString() {
		return
	}
	if actual == "" {
		*state = types.StringNull()
		return
	}
	*state = types.StringValue(actual)
}

// refreshLabelString updates an attribute nerdctl persists verbatim in a
// container label. Unlike refreshManagedString there is no image/runtime
// default to confuse: an absent label means unset, so unmanaged values are
// nulled and imports are filled in.
func refreshLabelString(state *types.String, actual string) {
	if actual == "" {
		*state = types.StringNull()
		return
	}
	if state.ValueString() != actual {
		*state = types.StringValue(actual)
	}
}

// refreshExtraHosts overwrites state extra_hosts when the entries recorded
// in the nerdctl/extraHosts label differ.
func refreshExtraHosts(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.extraHosts()

	current := map[string]string{}
	if !state.ExtraHosts.IsNull() {
		diags.Append(state.ExtraHosts.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.ExtraHosts = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.ExtraHosts = m
	return diags
}

// refreshMemory compares semantically — "512m" in config equals 536870912
// from inspect — and stores the byte count only on real drift.
func refreshMemory(state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.HostConfig.Memory

	if state.Memory.IsNull() {
		if actual > 0 {
			state.Memory = types.StringValue(strconv.FormatInt(actual, 10))
		}
		return diags
	}
	current, err := parseMemoryBytes(state.Memory.ValueString())
	if err != nil {
		diags.AddError("Invalid memory value in state", err.Error())
		return diags
	}
	if current == actual {
		return diags
	}
	if actual == 0 {
		state.Memory = types.StringNull()
		return diags
	}
	state.Memory = types.StringValue(strconv.FormatInt(actual, 10))
	return diags
}

func refreshCpus(state *containerResourceModel, info *containerInspect) {
	actual := info.cpus()
	// Quota is quantized to 1/period (period is typically 100000), so
	// round-tripped values can differ from the config by tiny fractions.
	const tolerance = 1e-4

	if state.Cpus.IsNull() {
		if actual > 0 {
			state.Cpus = types.Float64Value(actual)
		}
		return
	}
	if diff := state.Cpus.ValueFloat64() - actual; diff < tolerance && diff > -tolerance {
		return
	}
	if actual == 0 {
		state.Cpus = types.Float64Null()
		return
	}
	state.Cpus = types.Float64Value(actual)
}

// refreshPrivileged always writes a concrete value: the attribute is
// computed with a default, so unlike the managed-only strings it may never
// stay null (the import path arrives with it null).
func refreshPrivileged(state *containerResourceModel, info *containerInspect) {
	actual := info.HostConfig.Privileged
	if state.Privileged.IsNull() || state.Privileged.ValueBool() != actual {
		state.Privileged = types.BoolValue(actual)
	}
}

// refreshCaps overwrites a capability list when the reconstructed set
// differs. Comparison ignores order, case, and the CAP_ prefix; drifted
// values are written in CLI form (no prefix), sorted. Privileged containers
// hold every capability, so theirs are not tracked.
func refreshCaps(ctx context.Context, target *types.List, actual []string, privileged bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if privileged {
		return diags
	}
	var current []string
	if !target.IsNull() {
		diags.Append(target.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if capSetsEqual(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		*target = types.ListNull(types.StringType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, types.StringType, displayCaps(actual))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	*target = l
	return diags
}

// unprivilegedPortSysctl is injected by nerdctl into every container so it
// can bind low ports without CAP_NET_BIND_SERVICE. Like the default PATH in
// env, only prior state can tell it apart from user config.
const unprivilegedPortSysctl = "net.ipv4.ip_unprivileged_port_start"

// refreshSysctls overwrites state sysctls when the container's differ,
// ignoring the nerdctl-injected default unless the state manages that key.
func refreshSysctls(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := map[string]string{}
	maps.Copy(actual, info.HostConfig.Sysctls)

	current := map[string]string{}
	if !state.Sysctls.IsNull() {
		diags.Append(state.Sysctls.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if _, managed := current[unprivilegedPortSysctl]; !managed && actual[unprivilegedPortSysctl] == "0" {
		delete(actual, unprivilegedPortSysctl)
	}
	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Sysctls = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Sysctls = m
	return diags
}

// refreshTmpfs overwrites state tmpfs mounts when the container's differ.
// Option strings compare canonically — nerdctl merges user options into its
// noexec,nosuid,nodev defaults, so state keeps the configured spelling on a
// semantic match.
func refreshTmpfs(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.HostConfig.Tmpfs

	current := map[string]string{}
	if !state.Tmpfs.IsNull() {
		diags.Append(state.Tmpfs.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	equal := len(current) == len(actual)
	if equal {
		for dest, opts := range current {
			actualOpts, ok := actual[dest]
			if !ok || !tmpfsOptionsEqual(opts, actualOpts) {
				equal = false
				break
			}
		}
	}
	if equal {
		return diags
	}
	if len(actual) == 0 {
		state.Tmpfs = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Tmpfs = m
	return diags
}

// refreshLogConfig keeps the computed log_driver concrete (mirroring
// restart) and treats null log_opts as equal to empty.
func refreshLogConfig(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	driver := info.HostConfig.LogConfig.Driver
	if driver == "" {
		driver = "json-file"
	}
	if driver != state.LogDriver.ValueString() {
		state.LogDriver = types.StringValue(driver)
	}

	actual := info.HostConfig.LogConfig.Opts
	current := map[string]string{}
	if !state.LogOpts.IsNull() {
		diags.Append(state.LogOpts.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.LogOpts = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.LogOpts = m
	return diags
}

// refreshHealthcheck updates the healthcheck block only when the config
// manages it: images define healthchecks too, and an unset block means the
// image default applies (the user/hostname rule). Durations compare
// semantically — "1m" equals the 60000000000ns from inspect — and unset
// fields match the defaults nerdctl fills in at create time. no_healthcheck
// follows the same managed-only rule.
func refreshHealthcheck(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics

	if state.NoHealthcheck.ValueBool() && !info.healthcheckDisabled() {
		state.NoHealthcheck = types.BoolNull()
	}
	if state.Healthcheck.IsNull() {
		return diags
	}

	var hc healthcheckModel
	diags.Append(state.Healthcheck.As(ctx, &hc, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	actual := info.Config.Healthcheck
	if actual == nil || len(actual.Test) == 0 || info.healthcheckDisabled() {
		state.Healthcheck = types.ObjectNull(healthcheckObjectType.AttrTypes)
		return diags
	}

	changed := false
	if cmd := actual.command(); cmd != hc.Command.ValueString() {
		hc.Command = types.StringValue(cmd)
		changed = true
	}
	if refreshHealthDuration(&hc.Interval, actual.Interval, 30*time.Second) {
		changed = true
	}
	if refreshHealthDuration(&hc.Timeout, actual.Timeout, 30*time.Second) {
		changed = true
	}
	if refreshHealthDuration(&hc.StartPeriod, actual.StartPeriod, 0) {
		changed = true
	}
	if hc.Retries.IsNull() {
		if actual.Retries != 3 {
			hc.Retries = types.Int64Value(actual.Retries)
			changed = true
		}
	} else if hc.Retries.ValueInt64() != actual.Retries {
		hc.Retries = types.Int64Value(actual.Retries)
		changed = true
	}
	if !changed {
		return diags
	}
	obj, d := types.ObjectValueFrom(ctx, healthcheckObjectType.AttrTypes, hc)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	state.Healthcheck = obj
	return diags
}

// refreshHealthDuration reports whether it rewrote the field. A null field
// matches the given create-time default; a managed field matches any
// spelling of the same duration.
func refreshHealthDuration(field *types.String, actualNs int64, def time.Duration) bool {
	actual := time.Duration(actualNs)
	if field.IsNull() {
		if actual == def {
			return false
		}
	} else if current, err := time.ParseDuration(field.ValueString()); err == nil && current == actual {
		return false
	}
	*field = types.StringValue(actual.String())
	return true
}

// defaultSpecPathValue is containerd's PATH when neither the image nor the
// user sets one.
const defaultSpecPathValue = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// refreshEnv overwrites state env when the container's user variables
// differ. Runtime-injected entries are indistinguishable from user config
// except by prior state, so the default PATH and HOSTNAME are ignored
// unless the state already manages those keys.
func refreshEnv(ctx context.Context, state *containerResourceModel, info *containerInspect, imageEnv []string) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.userEnv(imageEnv)

	current := map[string]string{}
	if !state.Env.IsNull() {
		diags.Append(state.Env.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if _, managed := current["PATH"]; !managed && actual["PATH"] == defaultSpecPathValue {
		delete(actual, "PATH")
	}
	if _, managed := current["HOSTNAME"]; !managed {
		delete(actual, "HOSTNAME")
	}

	if maps.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Env = types.MapNull(types.StringType)
		return diags
	}
	m, d := types.MapValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	state.Env = m
	return diags
}

// refreshStringList overwrites a list attribute when the actual values
// differ. A null state and an empty actual compare equal, and order is
// significant — network order sets interface order, nameserver and search
// domain order set resolver precedence.
func refreshStringList(ctx context.Context, target *types.List, actual []string) diag.Diagnostics {
	var diags diag.Diagnostics
	var current []string
	if !target.IsNull() {
		diags.Append(target.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if slices.Equal(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		*target = types.ListNull(types.StringType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, types.StringType, actual)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	*target = l
	return diags
}

// refreshNetworks overwrites state networks when the container's attached
// networks differ. A null state matches the default bridge, so unconfigured
// containers on the default network never show drift.
func refreshNetworks(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	actual := info.networks()
	if state.Networks.IsNull() && len(actual) == 1 && actual[0] == "bridge" {
		return nil
	}
	return refreshStringList(ctx, &state.Networks, actual)
}

func refreshPorts(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual, err := info.portModels()
	if err != nil {
		diags.AddError("Failed to parse container ports", err.Error())
		return diags
	}

	var current []portModel
	if !state.Ports.IsNull() {
		diags.Append(state.Ports.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if portSetsEqual(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Ports = types.ListNull(portObjectType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, portObjectType, actual)
	diags.Append(d...)
	state.Ports = l
	return diags
}

func refreshVolumes(ctx context.Context, state *containerResourceModel, info *containerInspect) diag.Diagnostics {
	var diags diag.Diagnostics
	actual := info.volumeMounts()

	var current []volumeMountModel
	if !state.Volumes.IsNull() {
		diags.Append(state.Volumes.ElementsAs(ctx, &current, false)...)
		if diags.HasError() {
			return diags
		}
	}
	if mountSetsEqual(current, actual) {
		return diags
	}
	if len(actual) == 0 {
		state.Volumes = types.ListNull(volumeObjectType)
		return diags
	}
	l, d := types.ListValueFrom(ctx, volumeObjectType, actual)
	diags.Append(d...)
	state.Volumes = l
	return diags
}

// Update is unreachable: every attribute requires replacement.
func (r *containerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()

	// Stop before removing. A container with a restart policy (unless-stopped
	// by default) is racy to force-remove directly: containerd's restart
	// monitor can relaunch it in the window between rm's SIGKILL and the
	// restart-label update, orphaning a task that keeps holding the image.
	// That window is wide enough to lose when rm also has to tear a
	// healthcheck's transient systemd timer down over dbus. `stop` marks the
	// container explicitly-stopped, so the monitor leaves it alone and the
	// removal is deterministic. Best-effort: a missing or already-stopped
	// container is fine, and rm reports any real failure.
	_, _ = r.client.Run(ctx, "stop", name)

	if _, err := r.client.Run(ctx, "rm", "-f", name); err != nil && !nerdctl.NotFound(err) {
		resp.Diagnostics.AddError("Failed to remove container", err.Error())
		return
	}

	// Then wait until inspect no longer finds it, so a slow release cannot
	// race the removal of an image, volume, or network destroyed in the same
	// plan. Best-effort: the removal already succeeded.
	r.waitContainerGone(ctx, name)
}

// waitContainerGone polls until the named container is no longer inspectable,
// bounded so a container that never disappears (e.g. resurrected by a broken
// restart monitor) cannot hang the destroy indefinitely.
func (r *containerResource) waitContainerGone(ctx context.Context, name string) {
	const (
		timeout  = 30 * time.Second
		interval = time.Second
	)
	deadline := time.Now().Add(timeout)
	for {
		if _, err := r.client.Run(ctx, "container", "inspect", name); nerdctl.NotFound(err) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// ImportState imports by container name, e.g.
// `terraform import nerdctl_container.app app`. Read recovers every
// attribute except command, which is not present in inspect output, and
// healthcheck, which is indistinguishable from an image-defined one — set
// those in config to match the running container before applying.
func (r *containerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func buildRunArgs(ctx context.Context, plan *containerResourceModel) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	args := []string{"run", "-d", "--name", plan.Name.ValueString()}

	if restart := plan.Restart.ValueString(); restart != "" {
		args = append(args, "--restart", restart)
	}
	if e := plan.Entrypoint.ValueString(); e != "" {
		args = append(args, "--entrypoint", e)
	}
	if u := plan.User.ValueString(); u != "" {
		args = append(args, "--user", u)
	}
	if w := plan.Workdir.ValueString(); w != "" {
		args = append(args, "--workdir", w)
	}
	if h := plan.Hostname.ValueString(); h != "" {
		args = append(args, "--hostname", h)
	}
	if m := plan.Memory.ValueString(); m != "" {
		args = append(args, "--memory", m)
	}
	if !plan.Cpus.IsNull() {
		args = append(args, "--cpus", strconv.FormatFloat(plan.Cpus.ValueFloat64(), 'f', -1, 64))
	}
	if plan.Privileged.ValueBool() {
		args = append(args, "--privileged")
	}

	for _, cl := range []struct {
		flag string
		list types.List
	}{{"--cap-add", plan.CapAdd}, {"--cap-drop", plan.CapDrop}} {
		if cl.list.IsNull() {
			continue
		}
		var caps []string
		diags.Append(cl.list.ElementsAs(ctx, &caps, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, c := range caps {
			args = append(args, cl.flag, c)
		}
	}

	args, diags = appendMapFlags(ctx, args, diags, "--sysctl", plan.Sysctls)
	if diags.HasError() {
		return nil, diags
	}

	if !plan.Tmpfs.IsNull() {
		tmpfs := map[string]string{}
		diags.Append(plan.Tmpfs.ElementsAs(ctx, &tmpfs, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, dest := range slices.Sorted(maps.Keys(tmpfs)) {
			spec := dest
			if opts := tmpfs[dest]; opts != "" {
				spec += ":" + opts
			}
			args = append(args, "--tmpfs", spec)
		}
	}

	if d := plan.LogDriver.ValueString(); d != "" {
		args = append(args, "--log-driver", d)
	}
	args, diags = appendMapFlags(ctx, args, diags, "--log-opt", plan.LogOpts)
	if diags.HasError() {
		return nil, diags
	}

	if !plan.Healthcheck.IsNull() {
		var hc healthcheckModel
		diags.Append(plan.Healthcheck.As(ctx, &hc, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}
		args = append(args, "--health-cmd", hc.Command.ValueString())
		if v := hc.Interval.ValueString(); v != "" {
			args = append(args, "--health-interval", v)
		}
		if v := hc.Timeout.ValueString(); v != "" {
			args = append(args, "--health-timeout", v)
		}
		if !hc.Retries.IsNull() {
			args = append(args, "--health-retries", strconv.FormatInt(hc.Retries.ValueInt64(), 10))
		}
		if v := hc.StartPeriod.ValueString(); v != "" {
			args = append(args, "--health-start-period", v)
		}
	}
	if plan.NoHealthcheck.ValueBool() {
		args = append(args, "--no-healthcheck")
	}

	if !plan.Networks.IsNull() {
		var networks []string
		diags.Append(plan.Networks.ElementsAs(ctx, &networks, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, n := range networks {
			args = append(args, "--net", n)
		}
	}

	if v := plan.IP.ValueString(); v != "" {
		args = append(args, "--ip", v)
	}
	if v := plan.IP6.ValueString(); v != "" {
		args = append(args, "--ip6", v)
	}
	if v := plan.MacAddress.ValueString(); v != "" {
		args = append(args, "--mac-address", v)
	}

	if !plan.ExtraHosts.IsNull() {
		hosts := map[string]string{}
		diags.Append(plan.ExtraHosts.ElementsAs(ctx, &hosts, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, h := range slices.Sorted(maps.Keys(hosts)) {
			args = append(args, "--add-host", h+":"+hosts[h])
		}
	}

	if !plan.DNS.IsNull() {
		var dns []string
		diags.Append(plan.DNS.ElementsAs(ctx, &dns, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, d := range dns {
			args = append(args, "--dns", d)
		}
	}

	if !plan.DNSOpts.IsNull() {
		var dnsOpts []string
		diags.Append(plan.DNSOpts.ElementsAs(ctx, &dnsOpts, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, do := range dnsOpts {
			args = append(args, "--dns-option", do)
		}
	}

	if !plan.DNSSearch.IsNull() {
		var dnsSearch []string
		diags.Append(plan.DNSSearch.ElementsAs(ctx, &dnsSearch, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, ds := range dnsSearch {
			args = append(args, "--dns-search", ds)
		}
	}

	if !plan.Ports.IsNull() {
		var ports []portModel
		diags.Append(plan.Ports.ElementsAs(ctx, &ports, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, p := range ports {
			spec := fmt.Sprintf("%d:%d/%s", p.External.ValueInt64(), p.Internal.ValueInt64(), p.Protocol.ValueString())
			args = append(args, "-p", spec)
		}
	}

	args, diags = appendMapFlags(ctx, args, diags, "--label", plan.Labels)
	if diags.HasError() {
		return nil, diags
	}

	args, diags = appendMapFlags(ctx, args, diags, "-e", plan.Env)
	if diags.HasError() {
		return nil, diags
	}

	if !plan.Volumes.IsNull() {
		var mounts []volumeMountModel
		diags.Append(plan.Volumes.ElementsAs(ctx, &mounts, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, m := range mounts {
			hostPath := m.HostPath.ValueString()
			volumeName := m.VolumeName.ValueString()
			if (hostPath == "") == (volumeName == "") {
				diags.AddError(
					"Invalid volume mount",
					fmt.Sprintf("mount for %s: exactly one of host_path or volume_name must be set", m.ContainerPath.ValueString()),
				)
				return nil, diags
			}
			src := hostPath
			if src == "" {
				src = volumeName
			}
			spec := src + ":" + m.ContainerPath.ValueString()
			if m.ReadOnly.ValueBool() {
				spec += ":ro"
			}
			args = append(args, "-v", spec)
		}
	}

	args = append(args, plan.Image.ValueString())

	if !plan.Command.IsNull() {
		var command []string
		diags.Append(plan.Command.ElementsAs(ctx, &command, false)...)
		if diags.HasError() {
			return nil, diags
		}
		args = append(args, command...)
	}

	return args, diags
}

// appendMapFlags appends one flag per key=value entry, sorted by key so the
// argument order is deterministic.
func appendMapFlags(ctx context.Context, args []string, diags diag.Diagnostics, flag string, m types.Map) ([]string, diag.Diagnostics) {
	if m.IsNull() {
		return args, diags
	}
	kv := map[string]string{}
	diags.Append(m.ElementsAs(ctx, &kv, false)...)
	if diags.HasError() {
		return args, diags
	}
	for _, k := range slices.Sorted(maps.Keys(kv)) {
		args = append(args, flag, k+"="+kv[k])
	}
	return args, diags
}
