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
	store           *Store
	configFile      string
	hostIgnoreRegex *regexp.Regexp
}

// NewReconciler wires a Reconciler to the zone it manages, the store holding
// what it has already done, the Traefik config file it reads, and the regex of
// hostnames it must leave alone.
func NewReconciler(zone Zone, store *Store, configFile string, hostIgnoreRegex *regexp.Regexp) *Reconciler {
	return &Reconciler{
		zone:            zone,
		store:           store,
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

	// A snapshot is enough to decide what to do. Each change is committed on
	// its own as it succeeds, so a slow zone call cannot hold up the other
	// reconcile path, and neither path can overwrite the other's work with a
	// copy taken before it happened.
	s, err := r.store.Snapshot()
	if err != nil {
		return err
	}

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

		if err := r.store.RecordRouter(k, v); err != nil {
			return err
		}
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

		if err := r.store.ForgetRouter(k); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) CompareStateToWanIP(ctx context.Context, wanIP string) error {
	log.Debug().Msg("Comparing state file WAN IP to actual WAN IP")

	s, err := r.store.Snapshot()
	if err != nil {
		return err
	}

	if s.WanIP == wanIP {
		return nil
	}

	// Only persist the new IP to the state file once the update succeeds;
	// otherwise a transient failure would be masked on the next loop (no IP
	// change detected) and the DNS records would stay stale.
	if err := r.zone.SetIP(ctx, wanIP); err != nil {
		return err
	}

	return r.store.SetWanIP(wanIP)
}
