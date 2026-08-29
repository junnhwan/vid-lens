package main

import (
	"flag"
	"io"
)

type serverOptions struct {
	configPath string
}

func parseServerOptions(args []string) (serverOptions, error) {
	opts := serverOptions{configPath: "config.yaml"}
	flags := flag.NewFlagSet("vidlens-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.configPath, "config", opts.configPath, "config file path")
	if err := flags.Parse(args); err != nil {
		return serverOptions{}, err
	}
	return opts, nil
}
