package client

import (
	"os"
	"time"

	"github.com/spf13/cobra"
)

const longDescription = `
Kubernetes Command Line - kubecfg

OpenShift currently embeds the kubecfg command line for prototyping and debugging.
`

func NewCommandKubecfg(name string) *cobra.Command {
	cfg := &KubeConfig{}
	cmd := &cobra.Command{
		Use:   name,
		Short: "The Kubernetes command line client",
		Long:  longDescription + usage(name),
		Run: func(c *cobra.Command, args []string) {
			if len(args) < 1 {
				c.Help()
				os.Exit(1)
			}
			cfg.Args = args
			cfg.Run()
		},
	}
	flag := cmd.Flags()
	flag.BoolVar(&cfg.ServerVersion, "server_version", false, "Print the server's version number.")
	flag.BoolVar(&cfg.PreventSkew, "expect_version_match", false, "Fail if server's version doesn't match own version.")
	flag.StringVarP(&cfg.HttpServer, "host", "h", "", "The host to connect to.")
	flag.StringVarP(&cfg.Config, "config", "c", "", "Path to the config file.")
	flag.StringVarP(&cfg.Selector, "label", "l", "", "Selector (label query) to use for listing")
	flag.DurationVarP(&cfg.UpdatePeriod, "update", "u", 60*time.Second, "Update interval period")
	flag.StringVarP(&cfg.PortSpec, "port", "p", "", "The port spec, comma-separated list of <external>:<internal>,...")
	flag.IntVarP(&cfg.ServicePort, "service", "s", -1, "If positive, create and run a corresponding service on this port, only used with 'run'")
	flag.StringVar(&cfg.AuthConfig, "auth", os.Getenv("HOME")+"/.kubernetes_auth", "Path to the auth info file.  If missing, prompt the user.  Only used if doing https.")
	flag.BoolVar(&cfg.JSON, "json", false, "If true, print raw JSON for responses")
	flag.BoolVar(&cfg.YAML, "yaml", false, "If true, print raw YAML for responses")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "If true, print extra information")
	flag.BoolVar(&cfg.Proxy, "proxy", false, "If true, run a proxy to the api server")
	flag.StringVar(&cfg.WWW, "www", "", "If -proxy is true, use this directory to serve static files")
	flag.StringVar(&cfg.TemplateFile, "template_file", "", "If present, load this file as a golang template and use it for output printing")
	flag.StringVar(&cfg.TemplateStr, "template", "", "If present, parse this string as a golang template and use it for output printing")
	return cmd
}
