package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jetkvm/kvm"
)

func main() {
	versionPtr := flag.Bool("version", false, "print version and exit")
	versionJsonPtr := flag.Bool("version-json", false, "print version as json and exit")
	flag.Parse()

	if *versionPtr || *versionJsonPtr {
		versionData, err := kvm.GetVersionData(*versionJsonPtr)
		if err != nil {
			fmt.Printf("failed to get version data: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(versionData))
		return
	}

	kvm.Main()
}
