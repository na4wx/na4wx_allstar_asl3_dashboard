package allstarapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fullResponse is a trimmed, real response shape (confirmed against a
// live query to stats.allstarlink.org/api/stats/546054): one linked
// peer with a full registration record.
const fullResponse = `{
	"stats": {
		"node": 546054,
		"data": {
			"links": ["546056"],
			"keyed": false,
			"linkedNodes": [
				{
					"name": 546056,
					"callsign": "W5GLE",
					"Status": "Active",
					"server": {"Location": "Alvin, Texas SCR System Fusion"}
				}
			]
		}
	},
	"node": {"callsign": "W5GLE"}
}`

// mixedResponse mirrors a real response (node 2560) where most linked
// peers have only a bare "name" and no registration record at all --
// LinkedNodes doesn't cover every entry in Links.
const mixedResponse = `{
	"stats": {
		"node": 2560,
		"data": {
			"links": ["1070", "2287"],
			"keyed": false,
			"linkedNodes": [
				{"name": "1070"},
				{"name": 2287, "callsign": "K4FXC", "Status": "Active", "server": {"Location": "Wilmington, NC"}}
			]
		}
	},
	"node": {"callsign": "K6JSI"}
}`

func TestFetchNodeStatusFullyRegisteredPeer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/546054" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(fullResponse))
	}))
	defer ts.Close()

	status, err := FetchNodeStatus(context.Background(), ts.URL, "546054")
	if err != nil {
		t.Fatal(err)
	}
	if status.Callsign != "W5GLE" {
		t.Errorf("Callsign = %q, want W5GLE", status.Callsign)
	}
	if len(status.Peers) != 1 {
		t.Fatalf("Peers = %v, want 1 entry", status.Peers)
	}
	p := status.Peers[0]
	if p.Number != "546056" || p.Callsign != "W5GLE" || p.Status != "Active" || p.Location != "Alvin, Texas SCR System Fusion" {
		t.Errorf("Peers[0] = %+v, unexpected", p)
	}
}

func TestFetchNodeStatusMixedRegistration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mixedResponse))
	}))
	defer ts.Close()

	status, err := FetchNodeStatus(context.Background(), ts.URL, "2560")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Peers) != 2 {
		t.Fatalf("Peers = %v, want 2 entries", status.Peers)
	}
	if status.Peers[0].Number != "1070" || status.Peers[0].Callsign != "" {
		t.Errorf("Peers[0] = %+v, want bare number 1070 with no callsign", status.Peers[0])
	}
	if status.Peers[1].Number != "2287" || status.Peers[1].Callsign != "K4FXC" {
		t.Errorf("Peers[1] = %+v, want 2287/K4FXC", status.Peers[1])
	}
}

func TestFetchNodeStatusNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	_, err := FetchNodeStatus(context.Background(), ts.URL, "9999999")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFetchNodeStatusServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := FetchNodeStatus(context.Background(), ts.URL, "123")
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("a 500 should not be treated as ErrNotFound")
	}
}
