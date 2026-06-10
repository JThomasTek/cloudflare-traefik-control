package internal

import (
	"math"
	"os"
	"regexp"
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

func handleConfigChange(filename string, hostIgnoreRegex *regexp.Regexp) {
	log.Debug().Msg("Handling config change")
	traefikConfig, err := readTraefikConfig(filename)
	if err != nil {
		log.Error().Err(err).Msg("")
	}

	err = CompareStateToConfig(traefikConfig, hostIgnoreRegex)
	if err != nil {
		log.Error().Err(err).Msg("")
	}
}

func TraefikConfigWatcher(w *fsnotify.Watcher, filename string, hostIgnoreRegex *regexp.Regexp) {
	var (
		// Wait 100ms for new events; each new event resets the timer.
		waitTime = 100 * time.Millisecond

		// Keep track of the timers, as path -> timer.
		mu     sync.Mutex
		timers = make(map[string]*time.Timer)

		// Callback we run.
		eventHandler = func(e fsnotify.Event) {
			handleConfigChange(filename, hostIgnoreRegex)

			mu.Lock()
			delete(timers, e.Name)
			mu.Unlock()
		}
	)

	log.Debug().Msg("Starting Traefik config watcher")
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}

			if event.Name == filename && event.Has(fsnotify.Write) {
				mu.Lock()
				t, ok := timers[event.Name]
				mu.Unlock()

				if !ok {
					t = time.AfterFunc(math.MaxInt64, func() { eventHandler(event) })
					t.Stop()

					mu.Lock()
					timers[event.Name] = t
					mu.Unlock()
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

func InitialConfigCheck(filename string, hostIgnoreRegex *regexp.Regexp) error {
	log.Debug().Msg("Initial config check")
	traefikConfig, err := readTraefikConfig(filename)
	if err != nil {
		return err
	}

	err = CompareStateToConfig(traefikConfig, hostIgnoreRegex)
	if err != nil {
		return err
	}

	return nil
}
