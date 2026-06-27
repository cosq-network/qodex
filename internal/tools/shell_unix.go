//go:build !windows

package tools

func ShellCommand(command string) (string, []string) {
	return "sh", []string{"-c", command}
}
