package command

import (
	"bytes"
	"fmt"
	"os/exec"
	"os/user"
	"regexp"
	"strings"

	"github.com/google/shlex"
)

type (
	Output struct {
		Combined   string
		Normalized string
		Stdout     string
		Stderr     string
	}

	Outputs []Output
)

func (outputs Outputs) String() string {
	var sb strings.Builder
	for _, output := range outputs {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(output.Combined)
	}

	return sb.String()
}

func normalize(command string) string {
	command = strings.ReplaceAll(command, "\n", "")
	command = strings.ReplaceAll(command, "\\", " ")
	command = regexp.MustCompile(`\s+`).ReplaceAllString(command, " ")
	return strings.TrimSpace(command)
}

func getCommand(command string) (*exec.Cmd, error) {
	cmdAndArgs, err := shlex.Split(normalize(command))
	if err != nil {
		return nil, err
	}
	cmd := strings.TrimSpace(cmdAndArgs[0])
	if _, err := exec.LookPath(cmd); err != nil {
		return nil, fmt.Errorf(`"%s": %s`, command, err)
	}

	args := cmdAndArgs[1:]
	return exec.Command(cmd, args...), nil
}

func Run(command string) (Output, error) {
	cmd, err := getCommand(command)
	if err != nil {
		return Output{}, err
	}

	var o, e bytes.Buffer
	cmd.Stdout = &o
	cmd.Stderr = &e

	err = cmd.Run()
	output := Output{
		Stdout: o.String(),
		Stderr: e.String(),
	}
	output.Combined = output.Stderr + output.Stdout
	if strings.HasSuffix(output.Combined, "\n") {
		output.Normalized = output.Combined[:len(output.Combined)-1]
	} else {
		output.Normalized = output.Combined
	}

	if err != nil {
		return output, fmt.Errorf(`"%s": %s`, command, output.Combined)
	}

	return output, nil
}

func RunMultiple(commands ...string) (Outputs, error) {
	outputs := make(Outputs, len(commands))
	var err error
	for i, c := range commands {
		var output Output
		output, err = Run(c)
		outputs[i] = output
		if err != nil {
			break
		}
	}

	return outputs, err
}

func RunWithSudo(command string) (Output, error) {
	outputs, err := RunMultipleWithSudo(command)
	return outputs[0], err
}

func RunMultipleWithSudo(commands ...string) (Outputs, error) {
	if u, err := user.Current(); err == nil && u.Uid == "0" {
		return RunMultiple(commands...)
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return RunMultiple(commands...)
	}

	commandsWithSudo := make([]string, len(commands))
	for i, command := range commands {
		commandsWithSudo[i] = fmt.Sprintf("sudo --prompt='Sudo password:' -- %s", command)
	}

	return RunMultiple(commandsWithSudo...)
}
