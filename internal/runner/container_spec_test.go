package runner

import (
	"reflect"
	"testing"
)

func TestContainerSpecBasicRoundTrip(t *testing.T) {
	spec := ContainerSpec{Name: "mycontainer", Image: "myimage:latest"}
	got := spec.Build()
	want := []string{"run", "--rm", "--network=host", "--name", "mycontainer", "myimage:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build() = %v, want %v", got, want)
	}
}

func TestContainerSpecEmptyEnvProducesNoFlags(t *testing.T) {
	for _, env := range []map[string]string{nil, {}} {
		spec := ContainerSpec{Name: "n", Image: "img", Env: env}
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
	spec := ContainerSpec{Name: "n", Image: "img", Volumes: nil}
	args := spec.Build()
	for _, a := range args {
		if a == "-v" {
			t.Errorf("expected no -v flags for nil Volumes; got args: %v", args)
			break
		}
	}
}

func TestContainerSpecLabels(t *testing.T) {
	spec := ContainerSpec{
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
	spec := ContainerSpec{Name: "n", Image: "img", EnvFile: "/path/to/.env"}
	args := spec.Build()
	if !containsConsecutive(args, "--env-file", "/path/to/.env") {
		t.Errorf("expected --env-file /path/to/.env; got %v", args)
	}
}

func TestContainerSpecEnv(t *testing.T) {
	spec := ContainerSpec{
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
	spec := ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []VolumeMount{
			{Host: "/host/path", Container: "/container/path", Options: "z,ro"},
		},
	}
	args := spec.Build()
	if !containsConsecutive(args, "-v", "/host/path:/container/path:z,ro") {
		t.Errorf("expected -v /host/path:/container/path:z,ro; got %v", args)
	}
}

func TestContainerSpecVolumeWithoutOptions(t *testing.T) {
	spec := ContainerSpec{
		Name:  "n",
		Image: "img",
		Volumes: []VolumeMount{
			{Host: "/host/path", Container: "/container/path"},
		},
	}
	args := spec.Build()
	if !containsConsecutive(args, "-v", "/host/path:/container/path") {
		t.Errorf("expected -v /host/path:/container/path (no options); got %v", args)
	}
	// Ensure no trailing colon.
	for _, a := range args {
		if a == "/host/path:/container/path:" {
			t.Errorf("unexpected trailing colon in mount: %q", a)
		}
	}
}

func TestContainerSpecWorkDir(t *testing.T) {
	spec := ContainerSpec{Name: "n", Image: "img", WorkDir: "/workspace/myrepo"}
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
	spec := ContainerSpec{Name: "n", Image: "img", WorkDir: ""}
	args := spec.Build()
	for _, a := range args {
		if a == "-w" {
			t.Errorf("expected no -w when WorkDir is empty; got args: %v", args)
			break
		}
	}
}

func TestContainerSpecExtraFlags(t *testing.T) {
	spec := ContainerSpec{
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
	spec := ContainerSpec{
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
	spec := ContainerSpec{
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
	spec := ContainerSpec{
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
	spec := ContainerSpec{}
	got := spec.Build()
	// Should at minimum contain the fixed prefix tokens.
	if len(got) < 4 {
		t.Errorf("expected at least 4 args for zero-value spec; got %v", got)
	}
}

func TestContainerSpecFullArgs(t *testing.T) {
	spec := ContainerSpec{
		Runtime: "/opt/podman/bin/podman",
		Name:    "wallfacer-task-abc12345",
		Image:   "wallfacer:latest",
		Labels: map[string]string{
			"wallfacer.task.id":     "abc12345-1111-2222-3333-444444444444",
			"wallfacer.task.prompt": "fix the bug",
		},
		EnvFile: "/home/user/.wallfacer/.env",
		Env:     map[string]string{"CLAUDE_CODE_MODEL": "claude-opus-4-6"},
		Volumes: []VolumeMount{
			{Host: "claude-config", Container: "/home/claude/.claude"},
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
		"-v", "/repos/myproject:/workspace/myproject:z",
		"-v", "/instructions/CLAUDE.md:/workspace/CLAUDE.md:z,ro",
		"-w", "/workspace/myproject",
		"wallfacer:latest",
		"-p", "fix the bug", "--verbose", "--output-format", "stream-json",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build() mismatch:\ngot:  %v\nwant: %v", got, want)
	}
}
