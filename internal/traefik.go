package internal

import (
	"context"
	"math"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type Router struct {
	Rule string `yaml:"rule,omitempty"`
}

type TraefikConfig struct {
	HTTP struct {
		Routers map[string]Router `yaml:"routers,omitempty"`
	} `yaml:"http,omitempty"`
}

func readTraefikConfig(filename string) (TraefikConfig, error) {
	var config TraefikConfig

	log.Debug().Msg("Reading Traefik config file")
	// Read the config file
	data, err := os.ReadFile(filename)
	if err != nil {
		return config, err
	}

	// Unmarshal the config into a simplified TraefikConfig struct
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

func (r *Reconciler) handleConfigChange(ctx context.Context) {
	log.Debug().Msg("Handling config change")
	traefikConfig, err := readTraefikConfig(r.configFile)
	if err != nil {
		// Stop here rather than reconciling against the zero value. An
		// unreadable or half-written config parses to no routers at all, which
		// the reconcile would read as "every router was removed" and delete
		// every record CTC manages in the zone.
		log.Error().Err(err).Msg("")
		return
	}

	err = r.CompareStateToConfig(ctx, traefikConfig)
	if err != nil {
		log.Error().Err(err).Msg("")
	}
}

func (r *Reconciler) TraefikConfigWatcher(ctx context.Context, w *fsnotify.Watcher) {
	var (
		// Wait 100ms for new events; each new event resets the timer.
		waitTime = 100 * time.Millisecond

		// Keep track of the timers, as path -> timer.
		timersMu sync.Mutex
		timers   = make(map[string]*time.Timer)

		// Callback we run.
		eventHandler = func(e fsnotify.Event) {
			r.handleConfigChange(ctx)

			timersMu.Lock()
			delete(timers, e.Name)
			timersMu.Unlock()
		}
	)

	log.Debug().Msg("Starting Traefik config watcher")
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}

			if event.Name == r.configFile && event.Has(fsnotify.Write) {
				timersMu.Lock()
				t, ok := timers[event.Name]
				timersMu.Unlock()

				if !ok {
					t = time.AfterFunc(math.MaxInt64, func() { eventHandler(event) })
					t.Stop()

					timersMu.Lock()
					timers[event.Name] = t
					timersMu.Unlock()
				}

				t.Reset(waitTime)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}

			log.Error().Err(err).Msg("")
		}
	}
}

func (r *Reconciler) InitialConfigCheck(ctx context.Context) error {
	log.Debug().Msg("Initial config check")
	traefikConfig, err := readTraefikConfig(r.configFile)
	if err != nil {
		return err
	}

	err = r.CompareStateToConfig(ctx, traefikConfig)
	if err != nil {
		return err
	}

	return nil
}
