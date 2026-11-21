package main

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"

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

// CreateRecord ensures an A or AAAA record (chosen by IP family) for hostname
// points at addr.
func CreateRecord(cf *cloudflare.API, zoneID string, hostname string, addr netip.Addr) error {
	recordType := RecordTypeForIP(addr)
	ip := addr.String()
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Look up an existing record matching both name and type. Cloudflare allows
	// an A and an AAAA at the same name; we update only the one whose family
	// matches this IP.
	list := func() ([]cloudflare.DNSRecord, error) {
		recs, _, err := cf.ListDNSRecords(context.Background(), rc, cloudflare.ListDNSRecordsParams{
			Name: hostname,
			Type: recordType,
		})
		return recs, err
	}
	recs, err := list()
	if err != nil {
		return fmt.Errorf("failed to list DNS records: %w", err)
	}

	// Writing only on drift keeps concurrent instances from racing on every
	// record every run.
	if slices.ContainsFunc(recs, func(r cloudflare.DNSRecord) bool { return recordMatches(r, addr) }) {
		return nil
	}

	if len(recs) > 0 {
		// Prefer a record already holding addr, so correcting its TTL or proxy
		// setting does not repoint another record at the same name.
		target := recs[0]
		if i := slices.IndexFunc(recs, func(r cloudflare.DNSRecord) bool { return holdsAddr(r, addr) }); i >= 0 {
			target = recs[i]
		}
		_, err := cf.UpdateDNSRecord(context.Background(), rc, cloudflare.UpdateDNSRecordParams{
			ID:      target.ID,
			Type:    recordType,
			Name:    hostname,
			Content: ip,
			TTL:     1,
			Proxied: cloudflare.BoolPtr(false),
		})
		if err != nil {
			return fmt.Errorf("failed to update DNS record: %w", err)
		}
		return nil
	}

	_, createErr := cf.CreateDNSRecord(context.Background(), rc, cloudflare.CreateDNSRecordParams{
		Type:    recordType,
		Name:    hostname,
		Content: ip,
		TTL:     1,
		Proxied: cloudflare.BoolPtr(false),
	})
	if createErr != nil {
		// Another instance may have created the same record between the list
		// and the create, which Cloudflare rejects as a duplicate.
		recs, err := list()
		if err == nil && slices.ContainsFunc(recs, func(r cloudflare.DNSRecord) bool { return recordMatches(r, addr) }) {
			return nil
		}
		if err != nil {
			createErr = errors.Join(createErr, fmt.Errorf("failed to list DNS records after create: %w", err))
		}
		return fmt.Errorf("failed to create DNS record: %w", createErr)
	}

	return nil
}

// recordMatches reports whether r already holds addr with the TTL and proxy
// settings CreateRecord writes.
func recordMatches(r cloudflare.DNSRecord, addr netip.Addr) bool {
	return holdsAddr(r, addr) && r.TTL == 1 && (r.Proxied == nil || !*r.Proxied)
}

// holdsAddr compares parsed addresses: IPv6 content may not be in canonical
// form.
func holdsAddr(r cloudflare.DNSRecord, addr netip.Addr) bool {
	content, err := netip.ParseAddr(r.Content)
	return err == nil && content == addr
}

// DeleteRecord deletes a record, treating one that is already gone (e.g.
// pruned by another instance) as deleted.
func DeleteRecord(cf *cloudflare.API, zoneID string, recordID string) error {
	err := cf.DeleteDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), recordID)
	var notFound *cloudflare.NotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("failed to delete DNS record: %w", err)
	}

	return nil
}
