package sandbox

import (
	"fmt"
	"sort"
)

type BindType int

const (
	BindReadWrite BindType = iota
	BindReadOnly
	BindTmpFS
	BindSymlink
)

type Spec struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        []string

	Bind             []BindSpec
	UnshareNamespace bool
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
	for _, b := range s.Bind {
		switch b.Type {
		case BindReadOnly:
			allMappings = append(allMappings, pathMapping{
				b.Dst, b.Src, "--ro-bind",
			})
		case BindReadWrite:
			allMappings = append(allMappings, pathMapping{
				b.Dst, b.Src, "--bind",
			})
		case BindTmpFS:
			allMappings = append(allMappings, pathMapping{
				b.Dst, "", "--tmpfs",
			})
		case BindSymlink:
			allMappings = append(allMappings, pathMapping{
				b.Dst, b.Src, "--symlink",
			})
		}
	}
	sort.SliceStable(allMappings, func(i, j int) bool {
		return allMappings[i].dst < allMappings[j].dst
	})

	for i, m := range allMappings {
		if i+1 < len(allMappings) && allMappings[i+1].dst == m.dst {
			continue
		}
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

func (b *BindType) UnmarshalText(text []byte) error {
	str := string(text)
	switch str {
	case "rw":
		*b = BindReadWrite
	case "ro":
		*b = BindReadOnly
	case "tmpfs":
		*b = BindTmpFS
	case "symlink":
		*b = BindSymlink
	default:
		return fmt.Errorf("invalid bind-type: %s", str)
	}
	return nil
}
