package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// containerInspect is the subset of `nerdctl container inspect` output
// (dockercompat) the provider reads. Field shapes follow
// nerdctl/pkg/inspecttypes/dockercompat.
type containerInspect struct {
	ID    string `json:"Id"`
	Image string `json:"Image"`
	State struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
		Pid     int    `json:"Pid"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
		Memory         int64             `json:"Memory"`
		CPUQuota       int64             `json:"CPUQuota"`
		CPUPeriod      uint64            `json:"CPUPeriod"`
		DNS            []string          `json:"Dns"`
		DNSOptions     []string          `json:"DnsOptions"`
		DNSSearch      []string          `json:"DnsSearch"`
		Privileged     bool              `json:"Privileged"`
		CapAdd         []string          `json:"CapAdd"`
		CapDrop        []string          `json:"CapDrop"`
		Sysctls        map[string]string `json:"Sysctls"`
		Tmpfs          map[string]string `json:"Tmpfs"`
		ShmSize        int64             `json:"ShmSize"`
		ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
		Devices        []struct {
			PathOnHost        string `json:"PathOnHost"`
			PathInContainer   string `json:"PathInContainer"`
			CgroupPermissions string `json:"CgroupPermissions"`
		} `json:"Devices"`
		Ulimits []struct {
			Name string `json:"Name"`
			Soft int64  `json:"Soft"`
			Hard int64  `json:"Hard"`
		} `json:"Ulimits"`
		// LogConfig keys are lowercase in dockercompat output, unlike the
		// rest of HostConfig.
		LogConfig struct {
			Driver string            `json:"driver"`
			Opts   map[string]string `json:"opts"`
		} `json:"LogConfig"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	Config struct {
		Labels      map[string]string   `json:"Labels"`
		Env         []string            `json:"Env"`
		User        string              `json:"User"`
		Hostname    string              `json:"Hostname"`
		Healthcheck *healthcheckInspect `json:"Healthcheck"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func parseContainerInspect(out string) (*containerInspect, error) {
	var infos []containerInspect
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parsing container inspect output: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("empty container inspect result")
	}
	return &infos[0], nil
}

// restartPolicy recovers the --restart value. nerdctl stores it in the
// containerd restart-manager label; no label means the policy is "no".
func (ci *containerInspect) restartPolicy() string {
	if p := ci.Config.Labels["containerd.io/restart.policy"]; p != "" {
		return p
	}
	if n := ci.HostConfig.RestartPolicy.Name; n != "" && n != "no" {
		if n == "on-failure" && ci.HostConfig.RestartPolicy.MaximumRetryCount > 0 {
			return fmt.Sprintf("on-failure:%d", ci.HostConfig.RestartPolicy.MaximumRetryCount)
		}
		return n
	}
	return "no"
}

// userLabels recovers what the user passed via --label. Container labels
// also carry nerdctl/containerd bookkeeping, image-config derivations
// (io.containerd.image.config.*), and the image's own labels merged in —
// an image label only counts as user-set when its value was overridden.
func (ci *containerInspect) userLabels(imageLabels map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range ci.Config.Labels {
		if strings.HasPrefix(k, "nerdctl/") ||
			strings.HasPrefix(k, "containerd.io/") ||
			strings.HasPrefix(k, "io.containerd.image.config.") {
			continue
		}
		if iv, ok := imageLabels[k]; ok && iv == v {
			continue
		}
		out[k] = v
	}
	return out
}

// imageInspect is the subset of `nerdctl image inspect` output the provider
// reads: the image-defined labels and env to subtract from container state.
type imageInspect struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
		Env    []string          `json:"Env"`
	} `json:"Config"`
}

func parseImageInspect(out string) (*imageInspect, error) {
	var infos []imageInspect
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parsing image inspect output: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("empty image inspect result")
	}
	return &infos[0], nil
}

// userEnv recovers -e variables from the spec env, which merges runtime
// defaults, image ENV entries, and user variables. Image entries whose
// value the user did not override are subtracted. Runtime-injected PATH
// and HOSTNAME are left in: only prior state can tell them apart from
// user config, so refreshEnv handles them.
func (ci *containerInspect) userEnv(imageEnv []string) map[string]string {
	img := map[string]string{}
	for _, e := range imageEnv {
		k, v, _ := strings.Cut(e, "=")
		img[k] = v
	}
	out := map[string]string{}
	for _, e := range ci.Config.Env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if iv, exists := img[k]; exists && iv == v {
			continue
		}
		out[k] = v
	}
	return out
}

// envMap parses the full container environment (Config.Env) into a map. The
// container data source reports every variable, including image and runtime
// defaults — unlike the resource's userEnv, which subtracts image entries to
// recover only what the user set.
func (ci *containerInspect) envMap() map[string]string {
	out := map[string]string{}
	for _, e := range ci.Config.Env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

// portModels recovers published ports from NetworkSettings.Ports, which
// dockercompat keys as "<containerPort>/<proto>". Host IPs are collapsed:
// the provider publishes on all interfaces only, so dual-stack bindings of
// the same port dedupe to one entry. The result is sorted for determinism.
func (ci *containerInspect) portModels() ([]portModel, error) {
	seen := map[string]bool{}
	var out []portModel
	for spec, bindings := range ci.NetworkSettings.Ports {
		portPart, proto, ok := strings.Cut(spec, "/")
		if !ok {
			proto = "tcp"
		}
		internal, err := strconv.ParseInt(portPart, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("port spec %q: %w", spec, err)
		}
		for _, b := range bindings {
			if b.HostPort == "" {
				continue // exposed but not published
			}
			external, err := strconv.ParseInt(b.HostPort, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("host port %q: %w", b.HostPort, err)
			}
			key := fmt.Sprintf("%d:%d/%s", external, internal, proto)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, portModel{
				Internal: types.Int64Value(internal),
				External: types.Int64Value(external),
				Protocol: types.StringValue(proto),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Internal.ValueInt64() != out[j].Internal.ValueInt64() {
			return out[i].Internal.ValueInt64() < out[j].Internal.ValueInt64()
		}
		if out[i].Protocol.ValueString() != out[j].Protocol.ValueString() {
			return out[i].Protocol.ValueString() < out[j].Protocol.ValueString()
		}
		return out[i].External.ValueInt64() < out[j].External.ValueInt64()
	})
	return out, nil
}

// deviceModels recovers --device flags from inspect output (nerdctl derives
// them from its host-config label, so only user-passed devices appear).
// Sorted by host path for determinism.
func (ci *containerInspect) deviceModels() []deviceModel {
	var out []deviceModel
	for _, d := range ci.HostConfig.Devices {
		perms := d.CgroupPermissions
		if perms == "" {
			perms = "rwm"
		}
		cp := d.PathInContainer
		if cp == "" {
			cp = d.PathOnHost
		}
		out = append(out, deviceModel{
			HostPath:      types.StringValue(d.PathOnHost),
			ContainerPath: types.StringValue(cp),
			Permissions:   types.StringValue(perms),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].HostPath.ValueString() < out[j].HostPath.ValueString()
	})
	return out
}

// deviceSetsEqual compares device lists ignoring order. A null
// container_path counts as the host path, matching the CLI default.
func deviceSetsEqual(a, b []deviceModel) bool {
	key := func(d deviceModel) string {
		cp := d.ContainerPath.ValueString()
		if cp == "" {
			cp = d.HostPath.ValueString()
		}
		return d.HostPath.ValueString() + "|" + cp + "|" + d.Permissions.ValueString()
	}
	return keySetsEqual(a, b, key)
}

// ulimitModels recovers --ulimit flags, sorted by name for determinism.
func (ci *containerInspect) ulimitModels() []ulimitModel {
	var out []ulimitModel
	for _, u := range ci.HostConfig.Ulimits {
		out = append(out, ulimitModel{
			Name: types.StringValue(u.Name),
			Soft: types.Int64Value(u.Soft),
			Hard: types.Int64Value(u.Hard),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name.ValueString() < out[j].Name.ValueString()
	})
	return out
}

// ulimitSetsEqual compares ulimit lists ignoring order.
func ulimitSetsEqual(a, b []ulimitModel) bool {
	key := func(u ulimitModel) string {
		return fmt.Sprintf("%s|%d|%d", u.Name.ValueString(), u.Soft.ValueInt64(), u.Hard.ValueInt64())
	}
	return keySetsEqual(a, b, key)
}

// stopTimeout and platform recover their flags from labels. nerdctl writes
// nerdctl/stop-timeout only when the flag was passed, but nerdctl/platform
// always, normalized — so platform must be refreshed managed-only.
func (ci *containerInspect) stopTimeout() string { return ci.Config.Labels["nerdctl/stop-timeout"] }
func (ci *containerInspect) platform() string    { return ci.Config.Labels["nerdctl/platform"] }

// staticIP, staticIP6, and macAddress recover the --ip, --ip6, and
// --mac-address flags, which nerdctl persists verbatim in labels. Empty
// means the flag was not passed.
func (ci *containerInspect) staticIP() string   { return ci.Config.Labels["nerdctl/ip"] }
func (ci *containerInspect) staticIP6() string  { return ci.Config.Labels["nerdctl/ip6"] }
func (ci *containerInspect) macAddress() string { return ci.Config.Labels["nerdctl/mac-address"] }

// extraHosts recovers --add-host entries from the nerdctl/extraHosts label,
// a JSON array of "host:ip" strings, keyed by hostname. The cut is at the
// first colon: hostnames cannot contain one, IPv6 addresses can.
func (ci *containerInspect) extraHosts() map[string]string {
	raw := ci.Config.Labels["nerdctl/extraHosts"]
	if raw == "" {
		return nil
	}
	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	out := map[string]string{}
	for _, e := range entries {
		host, ip, ok := strings.Cut(e, ":")
		if !ok {
			continue
		}
		out[host] = ip
	}
	return out
}

// networks recovers the networks the container was attached to from the
// nerdctl/networks label, in --net order. Nil when the label is missing.
func (ci *containerInspect) networks() []string {
	raw := ci.Config.Labels["nerdctl/networks"]
	if raw == "" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil
	}
	return names
}

// volumeMounts recovers bind and named-volume mounts, excluding anonymous
// volumes created by image VOLUME directives (tracked in the
// nerdctl/anonymous-volumes label), which are not part of the configuration.
// The result is sorted by container path for determinism.
//
// nerdctl bind-mounts its managed resolv.conf, hosts, and hostname files
// into every container, and inspect output lists them when the container
// has no nerdctl/mounts label (no user mounts). They are skipped like
// docker does, at the cost of not tracking a deliberate user bind onto
// those destinations.
func (ci *containerInspect) volumeMounts() []volumeMountModel {
	anonymous := map[string]bool{}
	if raw := ci.Config.Labels["nerdctl/anonymous-volumes"]; raw != "" {
		var names []string
		if err := json.Unmarshal([]byte(raw), &names); err == nil {
			for _, n := range names {
				anonymous[n] = true
			}
		}
	}

	var out []volumeMountModel
	for _, m := range ci.Mounts {
		switch m.Destination {
		case "/etc/resolv.conf", "/etc/hosts", "/etc/hostname":
			continue
		}
		mount := volumeMountModel{
			ContainerPath: types.StringValue(m.Destination),
			HostPath:      types.StringNull(),
			VolumeName:    types.StringNull(),
			ReadOnly:      types.BoolValue(!m.RW),
		}
		switch m.Type {
		case "bind":
			mount.HostPath = types.StringValue(m.Source)
		case "volume":
			if anonymous[m.Name] {
				continue
			}
			mount.VolumeName = types.StringValue(m.Name)
		default:
			continue
		}
		out = append(out, mount)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ContainerPath.ValueString() < out[j].ContainerPath.ValueString()
	})
	return out
}

// healthcheckInspect mirrors nerdctl's healthcheck.Healthcheck: durations
// are time.Duration values serialized as nanosecond integers. nerdctl fills
// unset fields with its defaults (30s interval and timeout, 0 start period,
// 3 retries) at create time, so inspect output always carries concrete
// values when a healthcheck exists.
type healthcheckInspect struct {
	Test        []string `json:"Test"`
	Interval    int64    `json:"Interval"`
	Timeout     int64    `json:"Timeout"`
	StartPeriod int64    `json:"StartPeriod"`
	Retries     int64    `json:"Retries"`
}

// command recovers the --health-cmd value. CLI-configured checks are stored
// as ["CMD-SHELL", cmd]; exec-form tests from image config are joined for
// comparison.
func (h *healthcheckInspect) command() string {
	if len(h.Test) >= 2 && (h.Test[0] == "CMD-SHELL" || h.Test[0] == "CMD") {
		return strings.Join(h.Test[1:], " ")
	}
	return ""
}

// healthcheckDisabled reports whether health checking was explicitly turned
// off (--no-healthcheck stores Test as ["NONE"]).
func (ci *containerInspect) healthcheckDisabled() bool {
	hc := ci.Config.Healthcheck
	return hc != nil && len(hc.Test) > 0 && hc.Test[0] == "NONE"
}

// canonicalCap uppercases and strips the CAP_ prefix so config values like
// "net_admin" compare equal to the OCI "CAP_NET_ADMIN" that inspect
// reconstructs from the bounding set.
func canonicalCap(c string) string {
	return strings.TrimPrefix(strings.ToUpper(c), "CAP_")
}

// displayCaps converts OCI capability names to the CLI form used in config
// (no CAP_ prefix), sorted for determinism.
func displayCaps(caps []string) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, strings.TrimPrefix(c, "CAP_"))
	}
	sort.Strings(out)
	return out
}

// capSetsEqual compares capability lists ignoring order, case, and the CAP_
// prefix.
func capSetsEqual(a, b []string) bool {
	return keySetsEqual(a, b, canonicalCap)
}

// canonicalTmpfsOptions expands a tmpfs option string into its effective
// option set. nerdctl seeds every tmpfs mount with noexec,nosuid,nodev and
// lets user options override their counterparts, so a configured
// "size=64m,exec" and the "nosuid,nodev,size=64m,exec" reported by inspect
// canonicalize identically.
func canonicalTmpfsOptions(opts string) map[string]string {
	set := map[string]string{"noexec": "", "nosuid": "", "nodev": ""}
	conflicts := map[string]string{
		"exec": "noexec", "noexec": "exec",
		"suid": "nosuid", "nosuid": "suid",
		"dev": "nodev", "nodev": "dev",
		"rw": "ro", "ro": "rw",
	}
	for _, o := range strings.Split(opts, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if k, v, ok := strings.Cut(o, "="); ok {
			set[k] = v
			continue
		}
		if other, ok := conflicts[o]; ok {
			delete(set, other)
		}
		set[o] = ""
	}
	return set
}

func tmpfsOptionsEqual(a, b string) bool {
	return maps.Equal(canonicalTmpfsOptions(a), canonicalTmpfsOptions(b))
}

// cpus returns the CPU limit derived from the cgroup quota and period,
// or 0 when unlimited.
func (ci *containerInspect) cpus() float64 {
	if ci.HostConfig.CPUQuota <= 0 || ci.HostConfig.CPUPeriod == 0 {
		return 0
	}
	return float64(ci.HostConfig.CPUQuota) / float64(ci.HostConfig.CPUPeriod)
}

// parseMemoryBytes parses docker-style memory sizes — "512m", "1.5g",
// "1073741824" — with binary (1024) multipliers.
func parseMemoryBytes(s string) (int64, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimSuffix(v, "b")
	mult := int64(1)
	if len(v) > 0 {
		switch v[len(v)-1] {
		case 'k':
			mult = 1 << 10
		case 'm':
			mult = 1 << 20
		case 'g':
			mult = 1 << 30
		case 't':
			mult = 1 << 40
		}
		if mult > 1 {
			v = v[:len(v)-1]
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("invalid memory value %q", s)
	}
	return int64(f * float64(mult)), nil
}

// normalizeImageRef strips the implied docker.io registry prefixes so
// "traefik:v3" in config compares equal to the "docker.io/library/traefik:v3"
// containerd stores.
func normalizeImageRef(ref string) string {
	ref = strings.TrimPrefix(ref, "docker.io/library/")
	ref = strings.TrimPrefix(ref, "docker.io/")
	return ref
}

// portSetsEqual compares port lists ignoring order, so a semantically
// unchanged container does not dirty state over map iteration order.
func portSetsEqual(a, b []portModel) bool {
	key := func(p portModel) string {
		return fmt.Sprintf("%d:%d/%s", p.External.ValueInt64(), p.Internal.ValueInt64(), p.Protocol.ValueString())
	}
	return keySetsEqual(a, b, key)
}

// mountSetsEqual compares mount lists ignoring order.
func mountSetsEqual(a, b []volumeMountModel) bool {
	key := func(m volumeMountModel) string {
		return fmt.Sprintf("%s|%s|%s|%t",
			m.ContainerPath.ValueString(), m.HostPath.ValueString(), m.VolumeName.ValueString(), m.ReadOnly.ValueBool())
	}
	return keySetsEqual(a, b, key)
}

func keySetsEqual[T any](a, b []T, key func(T) string) bool {
	if len(a) != len(b) {
		return false
	}
	ka := make([]string, len(a))
	kb := make([]string, len(b))
	for i := range a {
		ka[i] = key(a[i])
		kb[i] = key(b[i])
	}
	sort.Strings(ka)
	sort.Strings(kb)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}
