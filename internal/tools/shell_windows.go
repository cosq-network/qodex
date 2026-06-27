//go:build windows

package tools

func ShellCommand(command string) (string, []string) {
	return "cmd.exe", []string{"/C", command}
}
