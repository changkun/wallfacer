package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

func TestContainerSpecBasicRoundTrip(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "mycontainer", Image: "myimage:latest"}
	got := spec.Build()
	want := []string{"run", "--rm", "--network=host", "--name", "mycontainer", "myimage:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build() = %v, want %v", got, want)
	}
}

// TestContainerSpecNetworkNone asserts that Network="none" produces --network=none.
func TestContainerSpecNetworkNone(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "c", Image: "img", Network: "none"}
	got := spec.Build()
	if !containsConsecutiveSingle(got, "--network=none") {
		t.Errorf("expected --network=none in args; got %v", got)
	}
	for _, a := range got {
		if a == "--network=host" {
			t.Errorf("unexpected --network=host when Network=none; got %v", got)
		}
	}
}

// TestContainerSpecNetworkEmptyFallback asserts that an empty Network falls back to --network=host.
func TestContainerSpecNetworkEmptyFallback(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "c", Image: "img", Network: ""}
	got := spec.Build()
	if !containsConsecutiveSingle(got, "--network=host") {
		t.Errorf("expected --network=host fallback when Network is empty; got %v", got)
	}
}

// containsConsecutiveSingle returns true if args contains the exact token s.
func containsConsecutiveSingle(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func TestContainerSpecEmptyEnvProducesNoFlags(t *testing.T) {
	for _, env := range []map[string]string{nil, {}} {
		spec := sandbox.ContainerSpec{Name: "n", Image: "img", Env: env}
		args := spec.Build()
		for _, a := range args {
			if a == "-e" {
				t.Errorf("expected no -e flags for empty Env; got args: %v", args)
				break
			}
		}
	}
}

func TestContainerSpecEmptyVolumesProducesNoFlags(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "n", Image: "img", Volumes: nil}
	args := spec.Build()
	for _, a := range args {
		if a == "-v" || a == "--mount" {
			t.Errorf("expected no -v or --mount flags for nil Volumes; got args: %v", args)
			break
		}
	}
}

func TestContainerSpecLabels(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:   "n",
		Image:  "img",
		Labels: map[string]string{"b": "2", "a": "1"},
	}
	args := spec.Build()
	if !containsConsecutive(args, "--label", "a=1") {
		t.Errorf("expected --label a=1; got %v", args)
	}
	if !containsConsecutive(args, "--label", "b=2") {
		t.Errorf("expected --label b=2; got %v", args)
	}
}

func TestContainerSpecEnvFile(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "n", Image: "img", EnvFile: "/path/to/.env"}
	args := spec.Build()
	if !containsConsecutive(args, "--env-file", "/path/to/.env") {
		t.Errorf("expected --env-file /path/to/.env; got %v", args)
	}
}

func TestContainerSpecEnv(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Env:   map[string]string{"FOO": "bar", "BAZ": "qux"},
	}
	args := spec.Build()
	if !containsConsecutive(args, "-e", "BAZ=qux") {
		t.Errorf("expected -e BAZ=qux; got %v", args)
	}
	if !containsConsecutive(args, "-e", "FOO=bar") {
		t.Errorf("expected -e FOO=bar; got %v", args)
	}
}

func TestContainerSpecVolumeWithOptions(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []sandbox.VolumeMount{
			{Host: "/host/path", Container: "/container/path", Options: "z,ro"},
		},
	}
	args := spec.Build()
	if !containsConsecutive(args, "--mount", "type=bind,src=/host/path,dst=/container/path,z,readonly") {
		t.Errorf("expected --mount type=bind,src=/host/path,dst=/container/path,z,readonly; got %v", args)
	}
}

func TestContainerSpecVolumeWithoutOptions(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []sandbox.VolumeMount{
			{Host: "/host/path", Container: "/container/path"},
		},
	}
	args := spec.Build()
	if !containsConsecutive(args, "--mount", "type=bind,src=/host/path,dst=/container/path") {
		t.Errorf("expected --mount type=bind,src=/host/path,dst=/container/path (no options); got %v", args)
	}
}

func TestContainerSpecVolumeWithColonInPath(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []sandbox.VolumeMount{
			{Host: "/path/with:colon", Container: "/workspace/myrepo", Options: "z"},
		},
	}
	args := spec.Build()
	// --mount syntax handles colons in paths without ambiguity (unlike -v).
	if !containsConsecutive(args, "--mount", "type=bind,src=/path/with:colon,dst=/workspace/myrepo,z") {
		t.Errorf("expected --mount with colon in path; got %v", args)
	}
}

func TestContainerSpecVolumeWithUnicodePath(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []sandbox.VolumeMount{
			{Host: "/home/user/我的项目", Container: "/workspace/我的项目", Options: "z"},
		},
	}
	args := spec.Build()
	if !containsConsecutive(args, "--mount", "type=bind,src=/home/user/我的项目,dst=/workspace/我的项目,z") {
		t.Errorf("expected --mount with unicode path; got %v", args)
	}
}

func TestContainerSpecWorkDir(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "n", Image: "img", WorkDir: "/workspace/myrepo"}
	args := spec.Build()
	if !containsConsecutive(args, "-w", "/workspace/myrepo") {
		t.Errorf("expected -w /workspace/myrepo; got %v", args)
	}
	// -w must appear before the image.
	wIdx, imgIdx := -1, -1
	for i, a := range args {
		if a == "-w" {
			wIdx = i
		}
		if a == "img" {
			imgIdx = i
		}
	}
	if wIdx == -1 || imgIdx == -1 || wIdx >= imgIdx {
		t.Errorf("-w (%d) should appear before image (%d) in %v", wIdx, imgIdx, args)
	}
}

func TestContainerSpecEmptyWorkDirOmitted(t *testing.T) {
	spec := sandbox.ContainerSpec{Name: "n", Image: "img", WorkDir: ""}
	args := spec.Build()
	for _, a := range args {
		if a == "-w" {
			t.Errorf("expected no -w when WorkDir is empty; got args: %v", args)
			break
		}
	}
}

func TestContainerSpecExtraFlags(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:       "n",
		Image:      "img",
		WorkDir:    "/work",
		ExtraFlags: []string{"--security-opt", "no-new-privileges"},
	}
	args := spec.Build()
	// ExtraFlags appear after -w and before image.
	wIdx, secIdx, imgIdx := -1, -1, -1
	for i, a := range args {
		if a == "-w" {
			wIdx = i
		}
		if a == "--security-opt" {
			secIdx = i
		}
		if a == "img" {
			imgIdx = i
		}
	}
	if secIdx == -1 {
		t.Fatalf("--security-opt not found in %v", args)
	}
	if wIdx >= secIdx || secIdx >= imgIdx {
		t.Errorf("expected -w (%d) < --security-opt (%d) < img (%d)", wIdx, secIdx, imgIdx)
	}
}

func TestContainerSpecCmd(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Cmd:   []string{"-p", "do something", "--verbose"},
	}
	args := spec.Build()
	// Image must come before Cmd.
	imgIdx := -1
	for i, a := range args {
		if a == "img" {
			imgIdx = i
		}
	}
	if imgIdx == -1 {
		t.Fatalf("image not found in %v", args)
	}
	cmdSection := args[imgIdx+1:]
	wantCmd := []string{"-p", "do something", "--verbose"}
	if !reflect.DeepEqual(cmdSection, wantCmd) {
		t.Errorf("Cmd section = %v, want %v", cmdSection, wantCmd)
	}
}

func TestContainerSpecLabelOrder(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:   "n",
		Image:  "img",
		Labels: map[string]string{"b": "2", "a": "1"},
	}
	args := spec.Build()
	// Find positions of --label a=1 and --label b=2.
	aIdx, bIdx := -1, -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--label" && args[i+1] == "a=1" {
			aIdx = i
		}
		if args[i] == "--label" && args[i+1] == "b=2" {
			bIdx = i
		}
	}
	if aIdx == -1 || bIdx == -1 {
		t.Fatalf("labels not found; args: %v", args)
	}
	if aIdx > bIdx {
		t.Errorf("expected --label a=1 before --label b=2; aIdx=%d bIdx=%d", aIdx, bIdx)
	}
}

func TestContainerSpecEnvOrder(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Name:  "n",
		Image: "img",
		Env:   map[string]string{"Z_KEY": "z", "A_KEY": "a"},
	}
	args := spec.Build()
	aIdx, zIdx := -1, -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-e" && args[i+1] == "A_KEY=a" {
			aIdx = i
		}
		if args[i] == "-e" && args[i+1] == "Z_KEY=z" {
			zIdx = i
		}
	}
	if aIdx == -1 || zIdx == -1 {
		t.Fatalf("env vars not found; args: %v", args)
	}
	if aIdx > zIdx {
		t.Errorf("expected -e A_KEY=a before -e Z_KEY=z; aIdx=%d zIdx=%d", aIdx, zIdx)
	}
}

func TestContainerSpecEmptyNameAllowed(t *testing.T) {
	// Zero-value struct must not panic.
	spec := sandbox.ContainerSpec{}
	got := spec.Build()
	// Should at minimum contain the fixed prefix tokens.
	if len(got) < 4 {
		t.Errorf("expected at least 4 args for zero-value spec; got %v", got)
	}
}

func TestContainerSpecFullArgs(t *testing.T) {
	spec := sandbox.ContainerSpec{
		Runtime: "/opt/podman/bin/podman",
		Name:    "wallfacer-task-abc12345",
		Image:   "wallfacer:latest",
		Labels: map[string]string{
			"wallfacer.task.id":     "abc12345-1111-2222-3333-444444444444",
			"wallfacer.task.prompt": "fix the bug",
		},
		EnvFile: "/home/user/.wallfacer/.env",
		Env:     map[string]string{"CLAUDE_CODE_MODEL": "claude-opus-4-6"},
		Volumes: []sandbox.VolumeMount{
			{Host: "claude-config", Container: "/home/claude/.claude", Named: true},
			{Host: "/repos/myproject", Container: "/workspace/myproject", Options: "z"},
			{Host: "/instructions/CLAUDE.md", Container: "/workspace/CLAUDE.md", Options: "z,ro"},
		},
		WorkDir: "/workspace/myproject",
		Cmd:     []string{"-p", "fix the bug", "--verbose", "--output-format", "stream-json"},
	}

	got := spec.Build()

	// Build() must NOT include the Runtime.
	for _, a := range got {
		if a == "/opt/podman/bin/podman" {
			t.Errorf("Runtime must not appear in Build() output; got %v", got)
		}
	}

	want := []string{
		"run", "--rm", "--network=host", "--name", "wallfacer-task-abc12345",
		"--label", "wallfacer.task.id=abc12345-1111-2222-3333-444444444444",
		"--label", "wallfacer.task.prompt=fix the bug",
		"--env-file", "/home/user/.wallfacer/.env",
		"-e", "CLAUDE_CODE_MODEL=claude-opus-4-6",
		"-v", "claude-config:/home/claude/.claude",
		"--mount", "type=bind,src=/repos/myproject,dst=/workspace/myproject,z",
		"--mount", "type=bind,src=/instructions/CLAUDE.md,dst=/workspace/CLAUDE.md,z,readonly",
		"-w", "/workspace/myproject",
		"wallfacer:latest",
		"-p", "fix the bug", "--verbose", "--output-format", "stream-json",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build() mismatch:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestMountOptsOnCurrentOS(t *testing.T) {
	// On Linux, "z" should be preserved. On other platforms, it should be stripped.
	isLinux := runtime.GOOS == "linux"

	tests := []struct {
		name string
		opts []string
		want string
	}{
		{"z only", []string{"z"}, ternary(isLinux, "z", "")},
		{"z and ro", []string{"z", "ro"}, ternary(isLinux, "z,ro", "ro")},
		{"ro only", []string{"ro"}, "ro"},
		{"empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mountOpts(tt.opts...)
			if got != tt.want {
				t.Errorf("mountOpts(%v) = %q, want %q", tt.opts, got, tt.want)
			}
		})
	}
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func TestCacheVolumeMountsPresent(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("WALLFACER_DEPENDENCY_CACHES=true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{EnvFile: envFile})
	spec := r.buildBaseContainerSpec("test-container", "", sandbox.Claude)

	found := map[string]bool{}
	for _, v := range spec.Volumes {
		if v.Named && strings.HasPrefix(v.Host, "wallfacer-cache-") {
			found[v.Container] = true
		}
	}
	for _, want := range []string{"/home/claude/.npm", "/home/claude/.cache/pip", "/home/claude/.cargo/registry", "/home/claude/.cache/go-build"} {
		if !found[want] {
			t.Errorf("expected cache volume for %s", want)
		}
	}
}

func TestCacheVolumesNotPresentWhenDisabled(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{EnvFile: envFile})
	spec := r.buildBaseContainerSpec("test-container", "", sandbox.Claude)

	for _, v := range spec.Volumes {
		if v.Named && strings.HasPrefix(v.Host, "wallfacer-cache-") {
			t.Errorf("unexpected cache volume: %s → %s", v.Host, v.Container)
		}
	}
}
