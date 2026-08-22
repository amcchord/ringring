package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amcchord/ringring/internal/provisioning"
	"github.com/amcchord/ringring/internal/telephony"
)

const (
	smokeUsernameA = "rr_smoke_a"
	smokePasswordA = "smoke-only-a-7Qm4s9Vx"
	smokeUsernameB = "rr_smoke_b"
	smokePasswordB = "smoke-only-b-2Kp8w6Nz"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: linphone-smoke-config <output-directory> <sip-server>")
		os.Exit(2)
	}
	outputDirectory := filepath.Clean(os.Args[1])
	configuration, err := telephony.Render([]telephony.DialDevice{
		{
			PartyID: "pty_smoke", DeviceID: "dev_smoke_a", Extension: "101",
			SIPUsername: smokeUsernameA, SIPSecret: smokePasswordA,
		},
		{
			PartyID: "pty_smoke", DeviceID: "dev_smoke_b", Extension: "102",
			SIPUsername: smokeUsernameB, SIPSecret: smokePasswordB,
		},
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	document, err := provisioning.LinphoneXML(provisioning.LinphoneConfig{
		Server: os.Args[2], Username: smokeUsernameA, Password: smokePasswordA, Extension: "101",
	})
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
		"linphone.xml": document,
	} {
		if err := os.WriteFile(filepath.Join(outputDirectory, name), content, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
