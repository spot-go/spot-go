package cmd

import (
	"testing"

	"github.com/spot-go/spot-go/internal/background"
)

func TestBackgroundCommandTakesItsArgs(t *testing.T) {
	cmd, rest, err := rootCmd.Find(background.Args(4242, "soloist-device"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd != backgroundCmd {
		t.Fatalf("spot-go %v runs %q, want %q", background.Args(4242, "soloist-device"), cmd.Name(), backgroundCmd.Name())
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatal(err)
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		t.Fatal(err)
	}
	if backgroundSoloist != 4242 || backgroundDevice != "soloist-device" {
		t.Fatalf("flags = %d, %q, want 4242, soloist-device", backgroundSoloist, backgroundDevice)
	}
	if !cmd.Hidden {
		t.Fatal("spot-go lists the background command in its help")
	}
}
