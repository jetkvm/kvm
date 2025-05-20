package main

import (
	"flag"
	"fmt"

	"github.com/jetkvm/kvm"
	"github.com/prometheus/common/version"
)

func printVersion() {
	version.Version = kvm.GetBuiltAppVersion()
	app_version := version.Print("JetKVM Application")
	fmt.Println(app_version)

	nativeVersion, err := kvm.GetNativeVersion()
	if err == nil {
		fmt.Println("\nJetKVM Native, version", nativeVersion)
	}
}

func main() {
	versionPtr := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionPtr {
		printVersion()
		return
	}

	kvm.Main()
}
