package sandbox

import (
	"sort"
)

type BindType int

const (
	BindReadWrite BindType = iota
	BindReadOnly
	BindTmpFS
)

type Spec struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        []string

	// namespace
	UnshareNamespace bool

	// paths
	ReadOnlyBind map[string]string
	Bind         map[string]string
	TmpFS        []string
}

type BindSpec struct {
	Src  string
	Dst  string
	Type BindType
}

func (s *Spec) commandArgs() (prefix []string, args []string, exe []string) {
	// env related
	if s.UnshareNamespace {
		prefix = append(prefix, "/usr/bin/env", "-i")
		prefix = append(prefix, s.Env...)
	} else {
		if len(s.Env) != 0 {
			prefix = append(prefix, "/usr/bin/env")
			prefix = append(prefix, s.Env...)
		}
	}

	// begin bwrap
	prefix = append(prefix, "bwrap")

	// mount related
	type pathMapping struct {
		dst  string
		src  string
		mode string
	}
	allMappings := []pathMapping{
		{"/etc/resolv.conf", "/etc/resolv.conf", "--ro-bind"},
		{"/sys", "/sys", "--ro-bind"},
		{"/run", "", "--tmpfs"},
		{"/dev", "", "--dev"},
		{"/proc", "", "--proc"},
	}
	for dst, src := range s.ReadOnlyBind {
		allMappings = append(allMappings, pathMapping{
			dst, src, "--ro-bind",
		})
	}
	for _, t := range s.TmpFS {
		allMappings = append(allMappings, pathMapping{
			t, "", "--tmpfs",
		})
	}
	for dst, src := range s.Bind {
		allMappings = append(allMappings, pathMapping{
			dst, src, "--bind",
		})
	}
	sort.Slice(allMappings, func(i, j int) bool {
		return allMappings[i].dst < allMappings[j].dst
	})
	for _, m := range allMappings {
		if m.src == "" {
			args = append(args, m.mode, m.dst)
		} else {
			args = append(args, m.mode, m.src, m.dst)
		}
	}

	// namespace related
	if s.UnshareNamespace {
		args = append(args,
			"--unshare-all",
			"--share-net",
			"--hostname", currentHostname,
			"--uid", currentUid,
			"--gid", currentGid,
		)
	}

	// command related
	if s.WorkingDir != "" {
		args = append(args, "--chdir", s.WorkingDir)
	}
	args = append(args,
		"--die-with-parent",
	)

	exe = append(exe, s.Command)
	exe = append(exe, s.Args...)
	return
}

func (s *Spec) CommandArgs() []string {
	p, a, e := s.commandArgs()
	return append(append(p, a...), e...)
}
