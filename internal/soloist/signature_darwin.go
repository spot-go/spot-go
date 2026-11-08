package soloist

import (
	"fmt"
	"os/exec"
	"strings"
)

const codeRequirement = `identifier "com.spotify.soloist" and anchor apple generic and certificate leaf[subject.OU] = "2FNC3A47ZF"`

func platformVerifySignature(bin string) error {
	out, err := exec.Command("codesign", "--verify", "--strict", "-R="+codeRequirement, bin).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded Soloist is not signed by Spotify: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
