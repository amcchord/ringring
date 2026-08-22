package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amcchord/ringring/internal/telephony"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: render-sip-smoke-config <output-directory>")
		os.Exit(2)
	}
	outputDirectory := filepath.Clean(os.Args[1])
	configuration, err := telephony.Render([]telephony.DialDevice{
		{
			PartyID: "pty_smoke", DeviceID: "dev_smoke_a", Extension: "101",
			SIPUsername: "rr_smoke_a", SIPSecret: "smoke-only-a-7Qm4s9Vx",
		},
		{
			PartyID: "pty_smoke", DeviceID: "dev_smoke_b", Extension: "102",
			SIPUsername: "rr_smoke_b", SIPSecret: "smoke-only-b-2Kp8w6Nz",
		},
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for name, content := range map[string][]byte{
		"pjsip.conf": configuration.PJSIP, "extensions.conf": configuration.Dialplan,
	} {
		if err := os.WriteFile(filepath.Join(outputDirectory, name), content, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
