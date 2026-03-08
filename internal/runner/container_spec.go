package runner

import "sort"

// VolumeMount describes a single -v HOST:CONTAINER[:OPTIONS] bind mount.
type VolumeMount struct {
	Host      string // host path or named volume (e.g. "claude-config")
	Container string // container path
	Options   string // e.g. "z,ro" or "z"; empty means no options suffix
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
	Cmd        []string          // appended after image
}

// Build returns the complete argument slice starting with "run".
// Flag order:
//
//	run --rm --network=host --name <Name>
//	[--label key=val ...]   (sorted)
//	[--env-file <EnvFile>]
//	[-e KEY=VAL ...]        (sorted)
//	[-v HOST:CONTAINER[:OPTIONS] ...]
//	[-w <WorkDir>]
//	[<ExtraFlags> ...]
//	<Image>
//	[<Cmd> ...]
func (s ContainerSpec) Build() []string {
	args := []string{"run", "--rm", "--network=host", "--name", s.Name}

	if len(s.Labels) > 0 {
		keys := make([]string, 0, len(s.Labels))
		for k := range s.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--label", k+"="+s.Labels[k])
		}
	}

	if s.EnvFile != "" {
		args = append(args, "--env-file", s.EnvFile)
	}

	if len(s.Env) > 0 {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+s.Env[k])
		}
	}

	for _, v := range s.Volumes {
		mount := v.Host + ":" + v.Container
		if v.Options != "" {
			mount += ":" + v.Options
		}
		args = append(args, "-v", mount)
	}

	if s.WorkDir != "" {
		args = append(args, "-w", s.WorkDir)
	}

	args = append(args, s.ExtraFlags...)
	args = append(args, s.Image)
	args = append(args, s.Cmd...)

	return args
}
