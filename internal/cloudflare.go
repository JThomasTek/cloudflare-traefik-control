package internal

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type cf struct {
	cloudflareAPI *cloudflare.API
	zoneID        string
}

var (
	cloudflareData cf
	commentMessage string = "Managed by ctc: "
	proxied        *bool  = new(bool)
)

func InitializeCloudflareAPIToken(token string, zoneID string) error {
	log.Debug().Msg("Initializing Cloudflare API with token")
	var err error

	cloudflareData.cloudflareAPI, err = cloudflare.NewWithAPIToken(token)
	cloudflareData.zoneID = zoneID
	*proxied = true

	return err
}

func AddSubdomain(routerIdentifier string, rule string, wanIP string) error {
	log.Info().Str("DNS_Record", routerIdentifier).Msg("Performing Cloudflare DNS add")

	ctx := context.Background()

	_, err := cloudflareData.cloudflareAPI.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.CreateDNSRecordParams{
		Type:    "A",
		Name:    rule,
		Content: wanIP,
		Comment: fmt.Sprintf("%s%s", commentMessage, routerIdentifier),
		TTL:     1,
		Proxied: proxied,
	})
	if err != nil {
		return err
	}

	return nil
}

func UpdateWanIP(s state) error {
	ctx := context.Background()

	records, _, err := cloudflareData.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return err
	}

	for _, record := range records {
		if len(record.Comment) >= 15 {
			substr := record.Comment[len(commentMessage):]
			_, ok := s.Routers[substr]
			if ok {
				if _, err := cloudflareData.cloudflareAPI.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.UpdateDNSRecordParams{
						ID:      record.ID,
						Content: s.WanIP,
					}); err != nil {
						log.Error().Err(err).Str("DNS_Record", substr).Msg("Failed to update DNS record")
					}
			}
		}
	}

	return nil
}

func ListDNSRecords() error {
	log.Debug().Msg("Listing Cloudflare DNS records")
	ctx := context.Background()

	records, _, err := cloudflareData.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return err
	}

	recordData, err := yaml.Marshal(records)
	if err != nil {
		return err
	}

	fmt.Printf("%s\n\n", string(recordData))

	return nil
}

func DeleteSubdomain(routerIdentifier string) error {
	log.Info().Str("DNS_Record", routerIdentifier).Msg("Performing Cloudflare DNS remove")

	ctx := context.Background()

	records, _, err := cloudflareData.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return err
	}

	for _, record := range records {
		if len(record.Comment) >= len(commentMessage) {
			substr := record.Comment[len(commentMessage):]
			if substr == routerIdentifier {
				err = cloudflareData.cloudflareAPI.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), record.ID)
				if err != nil {
					return err
				}

				break
			}
		}
	}

	return nil
}
