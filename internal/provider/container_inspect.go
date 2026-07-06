package provider

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// containerInspect is the subset of `nerdctl container inspect` output
// (dockercompat) the provider reads. Field shapes follow
// nerdctl/pkg/inspecttypes/dockercompat.
type containerInspect struct {
	ID         string `json:"Id"`
	Image      string `json:"Image"`
	HostConfig struct {
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	Config struct {
		Labels map[string]string `json:"Labels"`
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

// parseImageLabels extracts Config.Labels from `nerdctl image inspect`
// output, for subtracting image-defined labels from container labels.
func parseImageLabels(out string) (map[string]string, error) {
	var infos []struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return nil, fmt.Errorf("parsing image inspect output: %w", err)
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("empty image inspect result")
	}
	return infos[0].Config.Labels, nil
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
