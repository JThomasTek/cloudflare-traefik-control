package internal

import (
	"context"
	"fmt"
	"strings"

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

func InitializeCloudflareAPIKey(key string, email string, zoneID string) error {
	log.Debug().Msg("Initializing Cloudflare API with key")
	var err error

	cloudflareData.cloudflareAPI, err = cloudflare.New(key, email)
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

// parseRouterFromComment extracts the router identifier from a Cloudflare DNS
// record comment that CTC owns. It returns (identifier, true) only for comments
// carrying the commentMessage prefix, and ("", false) otherwise.
func parseRouterFromComment(comment string) (string, bool) {
	if !strings.HasPrefix(comment, commentMessage) {
		return "", false
	}

	return comment[len(commentMessage):], true
}

func UpdateWanIP(s state) error {
	ctx := context.Background()

	records, _, err := cloudflareData.cloudflareAPI.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return err
	}

	for _, record := range records {
		routerIdentifier, ok := parseRouterFromComment(record.Comment)
		if !ok {
			continue
		}

		if _, ok := s.Routers[routerIdentifier]; ok {
			_, err := cloudflareData.cloudflareAPI.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), cloudflare.UpdateDNSRecordParams{
				ID:      record.ID,
				Content: s.WanIP,
			})
			if err != nil {
				return err
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
		recordRouter, ok := parseRouterFromComment(record.Comment)
		if ok && recordRouter == routerIdentifier {
			err = cloudflareData.cloudflareAPI.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(cloudflareData.zoneID), record.ID)
			if err != nil {
				return err
			}

			break
		}
	}

	return nil
}
