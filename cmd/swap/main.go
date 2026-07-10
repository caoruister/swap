package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/lfaoro/swap/pkg/types"
	"github.com/urfave/cli/v3"
)

var BuildVersion string = "v0.0.1-dev"
var BuildDate string = "unset"
var BuildSHA string = "unset"
var APIURL string = "api.swapcli.com:443"

func main() {
	loadDotEnvIfExists(".env")

	args := append([]string(nil), os.Args...)
	args = applyBoolEnvFlag(args, "SWAP_DEBUG", "--debug", "-d")
	args = applyBoolEnvFlag(args, "SWAP_NO_LOGS", "--no-logs", "-n")
	args = applyBoolEnvFlag(args, "SWAP_LEGAL", "--legal", "--terms")

	// Allow overriding API URL from environment for testing (e.g. http://localhost:8081)
	if v := os.Getenv("SWAP_API_URL"); v != "" {
		APIURL = v
	}

	appcmd := &cli.Command{
		Authors: []any{
			map[string]string{
				"name":  "Leonardo Faoro",
				"email": "swap@leonardofaoro.com",
			},
		},
		Name:                   "swap",
		EnableShellCompletion:  true,
		UseShortOptionHandling: true,
		Suggest:                true,

		Version: fmt.Sprintf("Version %s\nBuild date: %s\nBuild SHA: %s", BuildVersion, BuildDate, BuildSHA),
		ExtraInfo: func() map[string]string {
			return map[string]string{
				"Build version": BuildVersion,
				"Build date":    BuildDate,
				"Build sha":     BuildSHA,
			}
		},

		Usage:     "Crypto Swaps Terminal",
		UsageText: `Swap is a Terminal-UI that facilitates secure cross-chain asset swaps.`,

		Before: func(c context.Context, cmd *cli.Command) (context.Context, error) {
			ctx := context.WithValue(c, types.APIURLKey, APIURL)
			return ctx, nil
		},
		Action: mainCmd,

		Commands: []*cli.Command{
			subCmd,
		},

		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "no-logs",
				Aliases:     []string{"n"},
				DefaultText: "do not store transaction logs",
				Value:       false,
				Action: func(context.Context, *cli.Command, bool) error {
					return nil
				},
			},
			&cli.BoolFlag{
				Name:    "legal",
				Aliases: []string{"terms"},
				Usage:   "print legal disclaimer",
				Action: func(context.Context, *cli.Command, bool) error {
					res := markdown.Render(legalDisclaimer, 80, 6)
					fmt.Println(string(res))
					os.Exit(0)
					return nil
				},
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "enable debug mode with verbose logging",
				Value:   false,
			},
		},
	}

	err := appcmd.Run(context.Background(), args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func loadDotEnvIfExists(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			continue
		}
		if _, exists := os.LookupEnv(k); exists {
			continue
		}
		_ = os.Setenv(k, v)
	}
}

func applyBoolEnvFlag(args []string, envKey string, longFlag string, aliases ...string) []string {
	v, ok := parseEnvBool(envKey)
	if !ok || !v {
		return args
	}
	flagNames := append([]string{longFlag}, aliases...)
	if hasFlag(args, flagNames...) {
		return args
	}
	return append(args, longFlag)
}

func parseEnvBool(key string) (bool, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false
	}
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off", "":
		return false, true
	default:
		return false, false
	}
}

func hasFlag(args []string, names ...string) bool {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		for n := range set {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}
