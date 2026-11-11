package soloist

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const maxVolume = 100

func SaveVolume(percent int) error {
	if percent < 0 || percent > maxVolume {
		return fmt.Errorf("invalid volume %d", percent)
	}
	if err := os.WriteFile(volumeFile, []byte(strconv.Itoa(percent)+"\n"), 0600); err != nil {
		return fmt.Errorf("fail to save the volume: %w", err)
	}
	return nil
}

func savedVolume() (int, bool) {
	b, err := os.ReadFile(volumeFile)
	if err != nil {
		return 0, false
	}
	percent, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || percent < 0 || percent > maxVolume {
		return 0, false
	}
	return percent, true
}
