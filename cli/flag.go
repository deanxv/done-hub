package cli

import (
	"done-hub/common/config"
	"done-hub/common/utils"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

var (
	port         = flag.Int("port", 0, "the listening port")
	printVersion = flag.Bool("version", false, "print version and exit")
	printHelp    = flag.Bool("help", false, "print help and exit")
	logDir       = flag.String("log-dir", "", "specify the log directory")
	Config       = flag.String("config", "config.yaml", "specify the config.yaml path")
	export       = flag.Bool("export", false, "Exports prices to a JSON file.")
)

func InitCli() {
	flag.Parse()

	if *printVersion {
		fmt.Println(config.Version)
		os.Exit(0)
	}

	if *printHelp {
		help()
		os.Exit(0)
	}

	if *port != 0 {
		viper.Set("port", *port)
	}

	if *logDir != "" {
		viper.Set("log_dir", *logDir)
	}

	if *export {
		ExportPrices()
		os.Exit(0)
	}

	if !utils.IsFileExist(*Config) {
		return
	}

	viper.SetConfigFile(*Config)
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

}

func help() {
	fmt.Println("Done Hub " + config.Version + " - Minimal backend service framework.")
	fmt.Println("Copyright (C) 2025 deanxv. All rights reserved.")
	fmt.Println("Original copyright holder: JustSong")
	fmt.Println("GitHub: https://github.com/deanxv/done-hub")
	fmt.Println("Usage: done-hub [--port <port>] [--log-dir <log directory>] [--config <config.yaml path>] [--version] [--help]")
}
