package protocol

type Request struct {
	Exec *ExecRequest
}

type ExecRequest struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        []string

	DisablePTY    bool
	TerminalName  string
	TerminalCols  int
	TerminalLines int
}

type Notification struct {
	WindowChange *WindowChange
	Exit         *ExitStatus
}

type ExitStatus struct {
	ExitCode int
}

type WindowChange struct {
	TerminalCols  int
	TerminalLines int
}
