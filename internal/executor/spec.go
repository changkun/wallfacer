package executor

// ContainerSpec is a declarative description of a host process launch: the
// agent binary is selected by Env["WALLFACER_AGENT"], invoked with Cmd, in
// WorkDir, with Env (and EnvFile) merged into its environment. Labels carry
// task metadata surfaced by List().
//
// The container-era fields (image, volumes, network, resource limits,
// entrypoint) were removed when wallfacer moved to host-only execution.
type ContainerSpec struct {
	Name    string            // process / handle name
	Labels  map[string]string // task metadata (e.g. wallfacer.task.id)
	EnvFile string            // env file merged into the child environment
	Env     map[string]string // env vars overlaid on top (wins on collision)
	WorkDir string            // child process working directory (host path)
	Cmd     []string          // agent argv (after harness BuildArgv)
}
