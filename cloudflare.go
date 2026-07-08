package main

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/cloudflare/cloudflare-go"
)

type Record struct {
	ID      string
	Name    string
	Type    string
	Content string
}

func GetZoneID(cf *cloudflare.API, zoneName string) (string, error) {
	zones, err := cf.ListZones(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to list zones: %w", err)
	}

	for _, z := range zones {
		if z.Name == zoneName {
			return z.ID, nil
		}
	}

	return "", fmt.Errorf("zone %s not found", zoneName)
}

func IterateRecords(cf *cloudflare.API, zoneID string, fn func(record Record) error) error {
	recs, _, err := cf.ListDNSRecords(context.Background(), cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return fmt.Errorf("failed to list DNS records: %w", err)
	}

	for _, r := range recs {
		r := Record{ID: r.ID, Name: r.Name, Type: r.Type, Content: r.Content}
		if err := fn(r); err != nil {
			// TODO better error handling
			return fmt.Errorf("error in callback for record %+v: %w", r, err)
		}
	}

	return nil
}

// RecordTypeForIP returns "A" for an IPv4 address and "AAAA" for an IPv6 address.
func RecordTypeForIP(ip netip.Addr) string {
	if ip.Is4() {
		return "A"
	}
	return "AAAA"
}

// CreateRecord upserts an A or AAAA record (chosen by IP family) and returns
// the record type it wrote.
func CreateRecord(cf *cloudflare.API, zoneID string, hostname string, ip string) (string, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "", fmt.Errorf("failed to parse IP %q: %w", ip, err)
	}
	recordType := RecordTypeForIP(addr)

	// Look up an existing record matching both name and type. Cloudflare allows
	// an A and an AAAA at the same name; we update only the one whose family
	// matches this IP.
	recs, _, err := cf.ListDNSRecords(context.Background(), cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
		Name: hostname,
		Type: recordType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list DNS records: %w", err)
	}

	if len(recs) > 0 {
		_, err := cf.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), cloudflare.UpdateDNSRecordParams{
			ID:      recs[0].ID,
			Type:    recordType,
			Name:    hostname,
			Content: ip,
			TTL:     1,
			Proxied: cloudflare.BoolPtr(false),
		})
		if err != nil {
			return "", fmt.Errorf("failed to update DNS record: %w", err)
		}
	} else {
		_, err := cf.CreateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), cloudflare.CreateDNSRecordParams{
			Type:    recordType,
			Name:    hostname,
			Content: ip,
			TTL:     1,
			Proxied: cloudflare.BoolPtr(false),
		})
		if err != nil {
			return "", fmt.Errorf("failed to create DNS record: %w", err)
		}
	}

	return recordType, nil
}

func DeleteRecord(cf *cloudflare.API, zoneID string, recordID string) error {
	err := cf.DeleteDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), recordID)
	if err != nil {
		return fmt.Errorf("failed to delete DNS record: %w", err)
	}

	return nil
}
