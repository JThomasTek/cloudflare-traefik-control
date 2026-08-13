package internal

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// Reconciler keeps a DNS zone in step with a Traefik config file and the host's
// WAN IP. It holds everything both reconcile paths need, so the config watcher
// and the WAN IP loop carry no configuration of their own.
type Reconciler struct {
	zone            Zone
	configFile      string
	hostIgnoreRegex *regexp.Regexp
}

// NewReconciler wires a Reconciler to the zone it manages, the Traefik config
// file it reads, and the regex of hostnames it must leave alone.
func NewReconciler(zone Zone, configFile string, hostIgnoreRegex *regexp.Regexp) *Reconciler {
	return &Reconciler{
		zone:            zone,
		configFile:      configFile,
		hostIgnoreRegex: hostIgnoreRegex,
	}
}

// cleanRule extracts the hostname from a Traefik router rule.
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

func (r *Reconciler) CompareStateToConfig(ctx context.Context, config TraefikConfig) error {
	log.Debug().Msg("Comparing state file to config")

	s, err := getState()
	if err != nil {
		return err
	}

	changed := false

	// Check if any new subdomains were added to the config
	for k, v := range config.HTTP.Routers {
		if _, ok := s.Routers[k]; ok {
			continue
		}

		host, err := cleanRule(v.Rule)
		if err != nil {
			log.Error().Err(err).Msg("")
			continue
		}

		// Only add subdomain if it doesn't match the ignore regex
		if r.hostIgnoreRegex.MatchString(host) {
			log.Debug().Msgf("Ignoring subdomain %s", host)
			continue
		}

		// Only record the router in the state file once the zone confirms the
		// record was created, so a failed add does not leave state out of sync.
		if err := r.zone.Add(ctx, k, host, s.WanIP); err != nil {
			log.Error().Err(err).Msg("")
			continue
		}

		s.Routers[k] = v
		changed = true
	}

	// Check if any subdomains were removed from the config
	for k := range s.Routers {
		if _, ok := config.HTTP.Routers[k]; ok {
			continue
		}

		// Only drop the router from the state file once the delete succeeds.
		if err := r.zone.Remove(ctx, k); err != nil {
			log.Error().Err(err).Msg("")
			continue
		}

		delete(s.Routers, k)
		changed = true
	}

	if changed {
		return writeState(s)
	}

	return nil
}

func (r *Reconciler) CompareStateToWanIP(ctx context.Context, wanIP string) error {
	log.Debug().Msg("Comparing state file WAN IP to actual WAN IP")

	s, err := getState()
	if err != nil {
		return err
	}

	if s.WanIP == wanIP {
		return nil
	}

	s.WanIP = wanIP

	// Only persist the new IP to the state file once the update succeeds;
	// otherwise a transient failure would be masked on the next loop (no IP
	// change detected) and the DNS records would stay stale.
	if err := r.zone.SetIP(ctx, wanIP); err != nil {
		return err
	}

	return writeState(s)
}
