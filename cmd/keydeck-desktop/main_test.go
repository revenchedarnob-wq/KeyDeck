package main

import (
	"os"
	"strings"
	"testing"
)

func TestProductionDesktopDoesNotPutRendererSecretIntoOSCommandLine(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"exec.Command", "func openURL", "Opener:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("production desktop source contains forbidden renderer-secret command-line path %q", forbidden)
		}
	}
}
