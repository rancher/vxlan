package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
)

var (
	// VERSION Of the binary
	VERSION = "0.0.0-dev"
)

func main() {
	app := cli.NewApp()
	app.Name = "rancher-vxlan"
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:   "debug, d",
			EnvVar: "RANCHER_DEBUG",
		},
	}
	app.Action = func(ctx *cli.Context) {
		if err := appMain(ctx); err != nil {
			logrus.Fatal(err)
		}
	}

	app.Run(os.Args)
}

func appMain(ctx *cli.Context) error {
	logrus.Infof("Started")
	if ctx.Bool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Debugf("Setting LogLevel to Debug")
	}

	return nil
}
