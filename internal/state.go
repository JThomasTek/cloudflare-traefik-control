package internal

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	stateFolder = "/etc/ctc/"
	stateFile   = stateFolder + "state.yml"
	mu          sync.Mutex
)

type state struct {
	WanIP   string
	Routers map[string]Router
}

func generateState() error {
	log.Debug().Msg("Generate the state file")

	// First check if directory exists and if not then create it
	if _, err := os.Stat(stateFolder); os.IsNotExist(err) {
		err = os.Mkdir(stateFolder, 0744)
		if err != nil {
			return err
		}
	}

	// Create the state file
	file, err := os.Create(stateFile)
	if err != nil {
		return err
	}

	defer file.Close()

	return nil
}

func getState() (state, error) {
	log.Debug().Msg("Reading state file data")

	// First check that state file exists
	_, err := os.Stat(stateFile)
	if err != nil {
		// If it doesn't exist, generate it
		err = generateState()

		if err != nil {
			return state{}, err
		}
	}

	var s state

	mu.Lock()
	buffer, err := os.ReadFile(stateFile)
	mu.Unlock()
	if err != nil {
		return s, err
	}

	err = yaml.Unmarshal(buffer, &s)
	if err != nil {
		return s, err
	}

	// Ensure Routers map is initialized (fresh file or empty state has nil map)
	if s.Routers == nil {
		s.Routers = make(map[string]Router)
	}

	return s, nil
}

func writeState(newState state) error {
	log.Debug().Msg("Writing to the state file")

	data, err := yaml.Marshal(newState)
	if err != nil {
		return err
	}

	mu.Lock()
	err = os.WriteFile(stateFile, data, 0644)
	mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func cleanRule(rule string) string {
	hostStartIndex := strings.Index(rule, "Host(`") + 6
	hostEndIndex := strings.Index(rule[hostStartIndex:], "`)") + hostStartIndex
	hostSubstr := rule[hostStartIndex:hostEndIndex]

	return hostSubstr
}

func CompareStateToConfig(config TraefikConfig, hostIgnoreRegex *regexp.Regexp) error {
	log.Debug().Msg("Comparing state file to config")

	s, err := getState()
	if err != nil {
		return err
	}

	changed := false

	// Check if any new subdomains were added to the config
	for k, v := range config.HTTP.Routers {
		_, ok := s.Routers[k]
		if !ok {
			// Only add subdomain if it doesn't match the ignore regex
			if !hostIgnoreRegex.MatchString(cleanRule(v.Rule)) {
				// Add the subdomain to the state file
				s.Routers[k] = v
				changed = true

				// Perform Cloudflare DNS add
				err = AddSubdomain(k, cleanRule(v.Rule), s.WanIP)
				if err != nil {
					log.Error().Err(err).Msg("")
				}
			} else {
				log.Debug().Msg(fmt.Sprintf("Ignoring subdomain %s", cleanRule(v.Rule)))
			}
		}
	}

	// Check if any subdomains were removed from the config
	for k := range s.Routers {
		_, ok := config.HTTP.Routers[k]
		if !ok {
			delete(s.Routers, k)
			changed = true

			// Perform Cloudflare DNS remove
			err = DeleteSubdomain(k)
			if err != nil {
				log.Error().Err(err).Msg("")
			}
		}
	}

	if changed {
		if err = writeState(s); err != nil {
			return err
		}
	}

	return nil
}

func CompareStateToWanIP(wanIP string) error {
	log.Debug().Msg("Comparing state file WAN IP to actual WAN IP")

	s, err := getState()
	if err != nil {
		return err
	}

	// Check if the WAN IP has changed, update it if it has
	if s.WanIP != wanIP {
		s.WanIP = wanIP

		// Update Cloudflare DNS records with new WAN IP
		err = UpdateWanIP(s)
		if err != nil {
			log.Error().Err(err).Msg("")
		}

		if err = writeState(s); err != nil {
			return err
		}
	}

	return nil
}
