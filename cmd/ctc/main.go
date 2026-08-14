package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"

	"github.com/JThomasTek/traefik-config-to-cloudflare/internal"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

/* TODO: 1. Create main infinite loop that checks for WAN IP changes or subdomain changes and updates accordingly -DONE
1.a Create a state file that stores the current WAN IP and subdomains -DONE
2. Add logging -DONE
3. Add host ignore regex
4. Add support for Docker labels
5. Add support for multiple domains
6. Add ability to disable WAN IP updates
*/

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	switch os.Getenv("LOG_LEVEL") {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	//-------- Retrieve configuration from environment variables --------//
	traefikConfigFile := "/etc/traefik/config.yml"

	if os.Getenv("TRAEFIK_CONFIG_FILE") != "" {
		traefikConfigFile = os.Getenv("TRAEFIK_CONFIG_FILE")
	}

	stateFile := internal.DefaultStateFile

	if os.Getenv("TRAEFIK_STATE_FILE") != "" {
		stateFile = os.Getenv("TRAEFIK_STATE_FILE")
	}

	hostIgnoreRegexString := "^$"

	if os.Getenv("TRAEFIK_HOST_IGNORE_REGEX") != "" {
		hostIgnoreRegexString = os.Getenv("TRAEFIK_HOST_IGNORE_REGEX")
	}

	hostIgnoreRegex, err := regexp.Compile(hostIgnoreRegexString)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		log.Fatal().Msg("No Cloudflare API token provided")
	}

	zone, err := internal.NewCloudflareZone(os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CLOUDFLARE_ZONE_ID"))
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	//-------- Finish configuration --------//

	store, err := internal.NewStore(stateFile)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	ctx := context.Background()
	reconciler := internal.NewReconciler(zone, store, traefikConfigFile, hostIgnoreRegex)

	// Run initial WAN IP check
	err = reconciler.InitialWanIPCheck(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	// Run initial config check
	if err = reconciler.InitialConfigCheck(ctx); err != nil {
		log.Fatal().Err(err).Msg("")
	}

	// Create file watcher. Config file will be added later.
	traefikConfigWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	defer traefikConfigWatcher.Close()

	// Start go routines for watching WAN IP and Traefik config changes
	log.Info().Msg("Watching for config changes")
	go reconciler.TraefikConfigWatcher(ctx, traefikConfigWatcher)
	go reconciler.WanIPCheck(ctx, 60)

	// Get config file info
	configFileInfo, err := os.Lstat(traefikConfigFile)
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	// Verify the file provided is not a directory
	if configFileInfo.IsDir() {
		log.Fatal().Msgf("%s is a directory\n", traefikConfigFile)
	}

	// Add the config file to the watcher
	err = traefikConfigWatcher.Add(filepath.Dir(traefikConfigFile))
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	// Run in an infinite loop
	select {}
}
