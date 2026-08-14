package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

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

	// Cancelled on SIGINT/SIGTERM so a `docker stop` unwinds the reconcile
	// loops instead of having the process killed part-way through one.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reconciler := internal.NewReconciler(zone, store, traefikConfigFile, hostIgnoreRegex)

	// Establish the starting position before anything starts watching. The WAN
	// IP comes first: reconciling the config without one defers every add.
	//
	// Both are fatal. Reconciliation is only ever driven by events from here on
	// (see docs/adr/0001), so there is nothing that would retry a failed start
	// — exiting hands that job to the container's restart policy.
	if err = reconciler.ReconcileWanIP(ctx); err != nil {
		log.Fatal().Err(err).Msg("")
	}

	if err = reconciler.ReconcileConfig(ctx); err != nil {
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
	go reconciler.WatchTraefikConfig(ctx, traefikConfigWatcher)
	go reconciler.WatchWanIP(ctx, 60*time.Second)

	// The config file itself is not checked here: the reconcile above already
	// read it, and failed fatally if it was missing, unreadable, or a
	// directory.
	//
	// The directory is what gets watched, not the file — editors and template
	// renderers replace config files rather than writing them in place, and a
	// watch on the old inode would go quiet after the first such write.
	// WatchTraefikConfig filters events back down to the config file.
	err = traefikConfigWatcher.Add(filepath.Dir(traefikConfigFile))
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}

	// Block until a signal cancels the context, then let the watchers unwind.
	<-ctx.Done()
	log.Info().Msg("Shutting down")
}
