package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestExecArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "minimal",
			got:  execArgs("web", []string{"sh", "-c", "echo hi"}, nil, "", "", false, false),
			want: []string{"exec", "web", "sh", "-c", "echo hi"},
		},
		{
			name: "all flags",
			got: execArgs("web", []string{"reload"}, []string{"A=1", "B=2"},
				"1000:1000", "/srv", true, true),
			want: []string{
				"exec", "-d", "--privileged",
				"--user", "1000:1000", "--workdir", "/srv",
				"-e", "A=1", "-e", "B=2",
				"web", "reload",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("args = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestSystemPruneArgs(t *testing.T) {
	if got, want := systemPruneArgs(false, false), []string{"system", "prune", "--force"}; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if got, want := systemPruneArgs(true, true), []string{"system", "prune", "--force", "--all", "--volumes"}; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestContainerStateArgs(t *testing.T) {
	m := containerStateActionModel{
		Container: types.StringValue("app"),
		Timeout:   types.Int64Null(),
		Signal:    types.StringNull(),
	}
	if got, want := containerStateArgs("start", &m), []string{"start", "app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}

	m.Timeout = types.Int64Value(5)
	m.Signal = types.StringValue("SIGINT")
	want := []string{"stop", "-t", "5", "-s", "SIGINT", "app"}
	if got := containerStateArgs("stop", &m); !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}
