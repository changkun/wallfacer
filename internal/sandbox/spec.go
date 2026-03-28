package sandbox

import (
	"runtime"
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
// The Runtime field is NOT included; the caller uses it as the exec binary path.
func (s ContainerSpec) Build() []string {
	// Default to host networking so the agent can reach the API server on localhost.
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

	// Emit volume mounts. Named volumes use the short -v syntax, while bind
	// mounts use the long --mount syntax which supports the "readonly" keyword
	// instead of the short "ro" option.
	for _, v := range s.Volumes {
		if v.Named {
			mount := v.Host + ":" + v.Container
			if v.Options != "" {
				mount += ":" + v.Options
			}
			args = append(args, "-v", mount)
		} else {
			hostPath := translateHostPath(v.Host, s.Runtime)
			var parts []string
			parts = append(parts, "type=bind", "src="+hostPath, "dst="+v.Container)
			if v.Options != "" {
				for opt := range strings.SplitSeq(v.Options, ",") {
					opt = strings.TrimSpace(opt)
					// Translate "ro" to "readonly" for --mount syntax compatibility.
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

// BuildCreate returns the argument slice for `podman create` with a sleep
// entrypoint. The container stays alive and subsequent invocations use
// `podman exec`. Unlike Build(), this omits --rm and replaces the agent
// command with a sleep entrypoint.
func (s ContainerSpec) BuildCreate() []string {
	network := s.Network
	if network == "" {
		network = "host"
	}
	args := []string{"create", "--network=" + network, "--name", s.Name,
		"--entrypoint", "sleep"}

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
			hostPath := translateHostPath(v.Host, s.Runtime)
			var parts []string
			parts = append(parts, "type=bind", "src="+hostPath, "dst="+v.Container)
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
	args = append(args, "infinity") // CMD: sleep entrypoint argument

	return args
}

// BuildExec returns the argument slice for `podman exec <name> <cmd...>`.
func BuildExec(containerName string, cmd []string) []string {
	args := make([]string, 0, 2+len(cmd))
	args = append(args, "exec", containerName)
	args = append(args, cmd...)
	return args
}

// translateHostPath converts a Windows host path to a container-visible path
// when running on Windows. On non-Windows systems this is a no-op.
//
// Docker Desktop maps C:\ to /c/, while Podman Desktop maps C:\ to /mnt/c/.
// The runtime binary path is used to detect which mapping to apply.
func translateHostPath(hostPath, runtimeBin string) string {
	if runtime.GOOS != "windows" {
		return hostPath
	}
	return translateWindowsPath(hostPath, runtimeBin)
}

// translateWindowsPath performs the actual Windows-to-container path
// translation. Split out from translateHostPath so tests can exercise the
// logic on any OS (it uses pure string operations, not filepath.VolumeName
// which is OS-dependent).
func translateWindowsPath(hostPath, runtimeBin string) string {
	// Only translate absolute Windows paths with drive letters (e.g., C:\Users\...).
	// Relative paths, UNC paths (\\server\share), and already-unix paths
	// are returned as-is with backslash-to-slash normalization.
	if hasDriveLetter(hostPath) {
		drive := strings.ToLower(string(hostPath[0]))
		rest := strings.ReplaceAll(hostPath[2:], `\`, "/")

		prefix := "/" + drive
		if isPodman(runtimeBin) {
			prefix = "/mnt/" + drive
		}
		return prefix + rest
	}

	return strings.ReplaceAll(hostPath, `\`, "/")
}

// hasDriveLetter reports whether path starts with a Windows drive letter
// (e.g., "C:" or "c:"). Uses pure string checks so it works on any OS.
func hasDriveLetter(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// isPodman returns true if the runtime binary path looks like Podman.
// Handles both forward and backslash separators so it works cross-platform.
func isPodman(runtimeBin string) bool {
	// Extract the last path component using both separator types,
	// since filepath.Base only recognizes the OS-native separator.
	base := runtimeBin
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return strings.HasPrefix(strings.ToLower(base), "podman")
}
