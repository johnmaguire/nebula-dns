package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/cloudflare/cloudflare-go"
)

// fakeCloudflare serves each request from handler and records
// "METHOD path body" for every call made.
func fakeCloudflare(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, call int)) (*cloudflare.API, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		handler(w, r, len(calls))
	}))
	t.Cleanup(srv.Close)

	cf, err := cloudflare.NewWithAPIToken("token", cloudflare.BaseURL(srv.URL), cloudflare.UsingRetryPolicy(0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	return cf, &calls
}

const (
	cfEmptyList = `{"success":true,"errors":[],"result":[]}`
	cfOneRecord = `{"success":true,"errors":[],"result":[{"id":"r1","type":"A","name":"h.example.com","content":"%s","ttl":1,"proxied":false}]}`
	cfDuplicate = `{"success":false,"errors":[{"code":81058,"message":"An identical record already exists."}],"result":null}`
	cfNotFound  = `{"success":false,"errors":[{"code":81044,"message":"Record does not exist."}],"result":null}`
)

func TestCreateRecord_SkipsWriteWhenUnchanged(t *testing.T) {
	cf, calls := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		fmt.Fprintf(w, cfOneRecord, "10.0.0.1")
	})

	if err := CreateRecord(cf, "zone", "h.example.com", netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "GET ") {
		t.Errorf("calls = %v, want a single GET", *calls)
	}
}

func TestCreateRecord_UpdatesOnDrift(t *testing.T) {
	cf, calls := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.Method == http.MethodPatch {
			fmt.Fprint(w, `{"success":true,"errors":[],"result":{"id":"r1"}}`)
			return
		}
		fmt.Fprintf(w, cfOneRecord, "10.0.0.2")
	})

	if err := CreateRecord(cf, "zone", "h.example.com", netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	wantPatch(t, *calls, "r1", "10.0.0.1")
}

func TestCreateRecord_UpdatesRecordHoldingAddress(t *testing.T) {
	cf, calls := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.Method == http.MethodPatch {
			fmt.Fprint(w, `{"success":true,"errors":[],"result":{"id":"r2"}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"errors":[],"result":[
			{"id":"r1","type":"A","name":"h.example.com","content":"10.0.0.2","ttl":1,"proxied":false},
			{"id":"r2","type":"A","name":"h.example.com","content":"10.0.0.1","ttl":120,"proxied":false}]}`)
	})

	if err := CreateRecord(cf, "zone", "h.example.com", netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	wantPatch(t, *calls, "r2", "10.0.0.1")
}

// wantPatch asserts calls is a GET followed by a PATCH of recordID to ip.
func wantPatch(t *testing.T, calls []string, recordID, ip string) {
	t.Helper()
	if len(calls) != 2 || !strings.HasPrefix(calls[1], "PATCH /zones/zone/dns_records/"+recordID+" ") ||
		!strings.Contains(calls[1], `"content":"`+ip+`"`) {
		t.Errorf("calls = %v, want GET then PATCH of %s to %s", calls, recordID, ip)
	}
}

func TestCreateRecord_LostCreateRaceIsSuccess(t *testing.T) {
	cf, _ := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, call int) {
		switch call {
		case 1:
			fmt.Fprint(w, cfEmptyList)
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, cfDuplicate)
		default:
			// Cloudflare applies the name and type filters; a re-list that
			// dropped them must not find the winner's record.
			if q := r.URL.Query(); q.Get("name") != "h.example.com" || q.Get("type") != "A" {
				fmt.Fprint(w, cfEmptyList)
				return
			}
			fmt.Fprintf(w, cfOneRecord, "10.0.0.1")
		}
	})

	if err := CreateRecord(cf, "zone", "h.example.com", netip.MustParseAddr("10.0.0.1")); err != nil {
		t.Errorf("CreateRecord() = %v, want nil", err)
	}
}

func TestCreateRecord_CreateFailureIsReported(t *testing.T) {
	cf, _ := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, call int) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, cfDuplicate)
			return
		}
		fmt.Fprint(w, cfEmptyList)
	})

	if err := CreateRecord(cf, "zone", "h.example.com", netip.MustParseAddr("10.0.0.1")); err == nil {
		t.Error("CreateRecord() = nil, want error")
	}
}

func TestDeleteRecord_AlreadyGoneIsSuccess(t *testing.T) {
	cf, _ := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, cfNotFound)
	})

	if err := DeleteRecord(cf, "zone", "r1"); err != nil {
		t.Errorf("DeleteRecord() = %v, want nil", err)
	}
}

func TestDeleteRecord_OtherErrorsAreReported(t *testing.T) {
	cf, _ := fakeCloudflare(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"result":null}`)
	})

	if err := DeleteRecord(cf, "zone", "r1"); err == nil {
		t.Error("DeleteRecord() = nil, want error")
	}
}
