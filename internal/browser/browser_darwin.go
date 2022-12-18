package browser

import "os/exec"

func open(url string) error {
	return exec.Command("open", url).Start()
}
