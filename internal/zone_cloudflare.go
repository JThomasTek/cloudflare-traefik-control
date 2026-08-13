package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog/log"
)

// cloudflareZone is the Zone adapter backed by the Cloudflare DNS API.
type cloudflareZone struct {
	api    *cloudflare.API
	zoneID string
}

// NewCloudflareZone builds a Zone backed by the Cloudflare API, authenticating
// with an API token.
func NewCloudflareZone(token string, zoneID string) (Zone, error) {
	log.Debug().Msg("Initializing Cloudflare API with token")

	api, err := cloudflare.NewWithAPIToken(token)
	if err != nil {
		return nil, err
	}

	return &cloudflareZone{api: api, zoneID: zoneID}, nil
}

func (z *cloudflareZone) Add(ctx context.Context, router string, host string, ip string) error {
	log.Info().Str("DNS_Record", router).Msg("Performing Cloudflare DNS add")

	// Proxied is a pointer in the Cloudflare params, so it needs an addressable
	// value rather than a literal.
	proxied := true

	_, err := z.api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(z.zoneID), cloudflare.CreateDNSRecordParams{
		Type:    "A",
		Name:    host,
		Content: ip,
		Comment: ownershipComment(router),
		TTL:     1, // 1 means "automatic" to Cloudflare.
		Proxied: &proxied,
	})

	return err
}

func (z *cloudflareZone) SetIP(ctx context.Context, ip string) error {
	owned, err := z.owned(ctx)
	if err != nil {
		return err
	}

	// Collect failures instead of returning on the first one: an individual
	// record that cannot be written must not stop the rest from moving to the
	// new address.
	var errs []error

	for router, recordID := range owned {
		log.Info().Str("DNS_Record", router).Str("WAN_IP", ip).Msg("Performing Cloudflare DNS update")

		_, err := z.api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(z.zoneID), cloudflare.UpdateDNSRecordParams{
			ID:      recordID,
			Content: ip,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("updating record for router %q: %w", router, err))
		}
	}

	return errors.Join(errs...)
}

func (z *cloudflareZone) Remove(ctx context.Context, router string) error {
	owned, err := z.owned(ctx)
	if err != nil {
		return err
	}

	recordID, ok := owned[router]
	if !ok {
		// Idempotent: no record means the desired state already holds.
		log.Debug().Str("DNS_Record", router).Msg("No Cloudflare record owned for router, nothing to remove")
		return nil
	}

	log.Info().Str("DNS_Record", router).Msg("Performing Cloudflare DNS remove")

	return z.api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(z.zoneID), recordID)
}

// owned lists the zone's DNS records and returns the identifiers of those
// carrying CTC's ownership mark, keyed by router name. ListDNSRecords paginates
// on its own when no page is requested, so this covers the whole zone rather
// than just the first hundred records.
func (z *cloudflareZone) owned(ctx context.Context) (map[string]string, error) {
	records, _, err := z.api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(z.zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return nil, err
	}

	owned := make(map[string]string, len(records))

	for _, record := range records {
		if router, ok := routerFromComment(record.Comment); ok {
			owned[router] = record.ID
		}
	}

	return owned, nil
}
