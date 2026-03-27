package sandbox

import "testing"

func TestTranslateWindowsPath_DockerDriveLetter(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		runtime string
		want    string
	}{
		{
			name:    "C drive",
			path:    `C:\Users\alice\project`,
			runtime: `C:\Program Files\Docker\docker.exe`,
			want:    "/c/Users/alice/project",
		},
		{
			name:    "D drive lowercase",
			path:    `d:\repos\myrepo`,
			runtime: "/usr/bin/docker",
			want:    "/d/repos/myrepo",
		},
		{
			name:    "drive root only",
			path:    `C:\`,
			runtime: "docker",
			want:    "/c/",
		},
		{
			name:    "drive root no trailing slash",
			path:    `C:`,
			runtime: "docker",
			want:    "/c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateWindowsPath(tt.path, tt.runtime)
			if got != tt.want {
				t.Errorf("translateWindowsPath(%q, %q) = %q, want %q", tt.path, tt.runtime, got, tt.want)
			}
		})
	}
}

func TestTranslateWindowsPath_PodmanDriveLetter(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		runtime string
		want    string
	}{
		{
			name:    "C drive with podman",
			path:    `C:\Users\alice\project`,
			runtime: `C:\Program Files\RedHat\Podman\podman.exe`,
			want:    "/mnt/c/Users/alice/project",
		},
		{
			name:    "podman unix path",
			path:    `D:\work\repo`,
			runtime: "/opt/podman/bin/podman",
			want:    "/mnt/d/work/repo",
		},
		{
			name:    "podman-machine variant",
			path:    `C:\code`,
			runtime: "podman-machine",
			want:    "/mnt/c/code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateWindowsPath(tt.path, tt.runtime)
			if got != tt.want {
				t.Errorf("translateWindowsPath(%q, %q) = %q, want %q", tt.path, tt.runtime, got, tt.want)
			}
		})
	}
}

func TestTranslateWindowsPath_NonDrivePaths(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		runtime string
		want    string
	}{
		{
			name:    "unix path passthrough",
			path:    "/home/user/project",
			runtime: "docker",
			want:    "/home/user/project",
		},
		{
			name:    "UNC path slash normalization",
			path:    `\\server\share\dir`,
			runtime: "docker",
			want:    "//server/share/dir",
		},
		{
			name:    "relative path",
			path:    `foo\bar\baz`,
			runtime: "docker",
			want:    "foo/bar/baz",
		},
		{
			name:    "already unix relative",
			path:    "foo/bar",
			runtime: "docker",
			want:    "foo/bar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateWindowsPath(tt.path, tt.runtime)
			if got != tt.want {
				t.Errorf("translateWindowsPath(%q, %q) = %q, want %q", tt.path, tt.runtime, got, tt.want)
			}
		})
	}
}

func TestTranslateWindowsPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		runtime string
		want    string
	}{
		{
			name:    "spaces in path",
			path:    `C:\Program Files\My Project\src`,
			runtime: "docker",
			want:    "/c/Program Files/My Project/src",
		},
		{
			name:    "unicode characters",
			path:    `C:\Users\alice\我的项目`,
			runtime: "docker",
			want:    "/c/Users/alice/我的项目",
		},
		{
			name:    "mixed separators",
			path:    `C:\Users/alice\project/src`,
			runtime: "docker",
			want:    "/c/Users/alice/project/src",
		},
		{
			name:    "empty path",
			path:    "",
			runtime: "docker",
			want:    "",
		},
		{
			name:    "single character",
			path:    "x",
			runtime: "docker",
			want:    "x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateWindowsPath(tt.path, tt.runtime)
			if got != tt.want {
				t.Errorf("translateWindowsPath(%q, %q) = %q, want %q", tt.path, tt.runtime, got, tt.want)
			}
		})
	}
}

func TestIsPodman(t *testing.T) {
	tests := []struct {
		runtime string
		want    bool
	}{
		{"/opt/podman/bin/podman", true},
		{`C:\Program Files\RedHat\Podman\podman.exe`, true},
		{"podman", true},
		{"podman-machine", true},
		{"/usr/bin/docker", false},
		{`C:\Program Files\Docker\docker.exe`, false},
		{"docker", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.runtime, func(t *testing.T) {
			got := isPodman(tt.runtime)
			if got != tt.want {
				t.Errorf("isPodman(%q) = %v, want %v", tt.runtime, got, tt.want)
			}
		})
	}
}

func TestHasDriveLetter(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{`C:\Users`, true},
		{`d:\repos`, true},
		{`Z:`, true},
		{"/unix/path", false},
		{`\\server\share`, false},
		{"relative", false},
		{"", false},
		{":", false},
		{"1:", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasDriveLetter(tt.path)
			if got != tt.want {
				t.Errorf("hasDriveLetter(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
