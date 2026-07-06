package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/kmaris/terraform-provider-nerdctl/internal/provider"
)

// version is set by goreleaser via ldflags on release builds.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/kmaris/nerdctl",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
