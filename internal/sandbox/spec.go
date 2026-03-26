package sandbox

import (
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/sortedkeys"
)

// VolumeMount describes a single bind mount passed to the container runtime.
type VolumeMount struct {
	Host      string // host path or named volume (e.g. "claude-config")
	Container string // container path
	Options   string // e.g. "z,ro" or "z"; empty means no options suffix
	Named     bool   // true for named volumes (use -v); false for bind mounts (use --mount)
}

// ContainerSpec is a declarative description of a container run invocation.
// Call Build() to obtain the arg slice for exec.Command(spec.Runtime, spec.Build()...).
type ContainerSpec struct {
	Runtime    string            // binary path — NOT included in Build() output
	Name       string            // --name
	Image      string            // placed after volumes/workdir/extra flags
	Labels     map[string]string // --label key=val (sorted by key)
	EnvFile    string            // --env-file (omitted when empty)
	Env        map[string]string // -e KEY=VAL (sorted by key)
	Volumes    []VolumeMount     // -v mounts (insertion order preserved)
	WorkDir    string            // -w workdir (omitted when empty)
	ExtraFlags []string          // inserted between last -v/-w and image
	// Network controls the --network flag passed to the container runtime.
	// An empty string defaults to "host".
	Network string // --network (defaults to "host" when empty)
	// Resource limits — zero values mean no limit is passed to the runtime.
	CPUs   string   // e.g. "2.0" → --cpus 2.0
	Memory string   // e.g. "4g"  → --memory 4g
	Cmd    []string // appended after image
}

// Build returns the complete argument slice starting with "run".
func (s ContainerSpec) Build() []string {
	network := s.Network
	if network == "" {
		network = "host"
	}
	args := []string{"run", "--rm", "--network=" + network, "--name", s.Name}

	for k := range sortedkeys.Of(s.Labels) {
		args = append(args, "--label", k+"="+s.Labels[k])
	}

	if s.EnvFile != "" {
		args = append(args, "--env-file", s.EnvFile)
	}

	for k := range sortedkeys.Of(s.Env) {
		args = append(args, "-e", k+"="+s.Env[k])
	}

	for _, v := range s.Volumes {
		if v.Named {
			mount := v.Host + ":" + v.Container
			if v.Options != "" {
				mount += ":" + v.Options
			}
			args = append(args, "-v", mount)
		} else {
			var parts []string
			parts = append(parts, "type=bind", "src="+v.Host, "dst="+v.Container)
			if v.Options != "" {
				for opt := range strings.SplitSeq(v.Options, ",") {
					opt = strings.TrimSpace(opt)
					if opt == "ro" {
						parts = append(parts, "readonly")
					} else if opt != "" {
						parts = append(parts, opt)
					}
				}
			}
			args = append(args, "--mount", strings.Join(parts, ","))
		}
	}

	if s.WorkDir != "" {
		args = append(args, "-w", s.WorkDir)
	}

	if s.CPUs != "" {
		args = append(args, "--cpus", s.CPUs)
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}

	args = append(args, s.ExtraFlags...)
	args = append(args, s.Image)
	args = append(args, s.Cmd...)

	return args
}
