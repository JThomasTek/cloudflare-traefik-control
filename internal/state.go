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

	// Cloudflare operations are referenced through these vars so tests can
	// stub them without making real API calls.
	addSubdomain    = AddSubdomain
	deleteSubdomain = DeleteSubdomain
	updateWanIP     = UpdateWanIP
)

type state struct {
	WanIP   string
	Routers map[string]Router
}

func generateState() error {
	log.Debug().Msg("Generate the state file")

	// First check if directory exists and if not then create it
	if _, err := os.Stat(stateFolder); os.IsNotExist(err) {
		err = os.Mkdir(stateFolder, 0700)
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

	// Ensure the map is always usable so callers can assign to it without
	// risking a nil-map panic (e.g. on a freshly created/empty state file).
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
	err = os.WriteFile(stateFile, data, 0600)
	mu.Unlock()
	if err != nil {
		return err
	}

	return nil
}

func cleanRule(rule string) (string, error) {
	const hostMarker = "Host(`"

	markerIndex := strings.Index(rule, hostMarker)
	if markerIndex == -1 {
		return "", fmt.Errorf("rule %q does not contain a Host(`...`) clause", rule)
	}

	hostStartIndex := markerIndex + len(hostMarker)
	endOffset := strings.Index(rule[hostStartIndex:], "`)")
	if endOffset == -1 {
		return "", fmt.Errorf("rule %q has an unterminated Host(`...`) clause", rule)
	}

	return rule[hostStartIndex : hostStartIndex+endOffset], nil
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
			host, err := cleanRule(v.Rule)
			if err != nil {
				log.Error().Err(err).Msg("")
				continue
			}

			// Only add subdomain if it doesn't match the ignore regex
			if !hostIgnoreRegex.MatchString(host) {
				// Perform Cloudflare DNS add; only record the router in the
				// state file once Cloudflare confirms the record was created,
				// so a failed add does not leave state out of sync.
				if err = addSubdomain(k, host, s.WanIP); err != nil {
					log.Error().Err(err).Msg("")
					continue
				}

				s.Routers[k] = v
				changed = true
			} else {
				log.Debug().Msg(fmt.Sprintf("Ignoring subdomain %s", host))
			}
		}
	}

	// Check if any subdomains were removed from the config
	for k := range s.Routers {
		_, ok := config.HTTP.Routers[k]
		if !ok {
			// Perform Cloudflare DNS remove; only drop the router from the
			// state file once the delete succeeds.
			if err = deleteSubdomain(k); err != nil {
				log.Error().Err(err).Msg("")
				continue
			}

			delete(s.Routers, k)
			changed = true
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

		// Update Cloudflare DNS records with the new WAN IP. Only persist the
		// new IP to the state file once the update succeeds; otherwise a
		// transient failure would be masked on the next loop (no IP change
		// detected) and the DNS records would stay stale.
		if err = updateWanIP(s); err != nil {
			return err
		}

		if err = writeState(s); err != nil {
			return err
		}
	}

	return nil
}
