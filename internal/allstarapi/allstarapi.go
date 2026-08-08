// Package allstarapi queries AllStarLink's own public node-status
// service (https://stats.allstarlink.org) for a single node's currently
// linked peers. This is the only way this dashboard has to see a
// *remote* node's own connections -- there's no local Asterisk CLI to
// ask, since that node isn't hosted here. Reporting isn't guaranteed:
// plenty of real nodes never enable it (rpt.conf's own "eventlogfile"
// stats-reporting section is opt-in), which this package surfaces as
// ErrNotFound rather than an error, since it's an expected, common
// outcome, not a broken request.
package allstarapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DefaultBaseURL is AllStarLink's own published node-stats endpoint.
// Confirmed against a real query (https://stats.allstarlink.org/api/stats/546054)
// -- one node number appended per request; there's no batch form.
const DefaultBaseURL = "https://stats.allstarlink.org/api/stats"

// ErrNotFound means the node has never reported status to
// stats.allstarlink.org (reporting is opt-in on the node's own end) or
// the number doesn't exist -- confirmed both return a bare 404, with no
// JSON body to distinguish them further.
var ErrNotFound = errors.New("allstarapi: node not found or has never reported status")

// Peer is one node currently linked to the node a NodeStatus describes.
// Callsign/Status/Location come from AllStarLink's own registration
// database and are empty for a peer that isn't registered there (a
// real, common case -- confirmed against a live query returning
// linkedNodes entries with only a bare node number and nothing else).
type Peer struct {
	Number   string
	Callsign string
	Status   string
	Location string
}

// NodeStatus is one node's resolved status: its own registered
// callsign (empty if unregistered), whether it's currently keyed
// (transmitting), and the peers it's currently linked to.
type NodeStatus struct {
	Node     string
	Callsign string
	Keyed    bool
	Peers    []Peer
}

// flexString decodes a JSON field that stats.allstarlink.org sometimes
// sends as a bare number and sometimes as a quoted string -- confirmed
// on a real response, where linkedNodes[].name is a JSON number for
// entries with a full registration record and a JSON string for the
// "just a bare node number, never registered" entries.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

type rawLinkedNode struct {
	Name     flexString `json:"name"`
	Callsign string     `json:"callsign"`
	Status   string     `json:"Status"`
	Server   struct {
		Location string `json:"Location"`
	} `json:"server"`
}

type rawResponse struct {
	Stats struct {
		Data struct {
			// Links is the authoritative, always-just-numbers list of
			// currently connected peers -- LinkedNodes below only
			// enriches entries it happens to have a registration
			// record for, and isn't guaranteed to list every peer.
			Links       []string        `json:"links"`
			Keyed       bool            `json:"keyed"`
			LinkedNodes []rawLinkedNode `json:"linkedNodes"`
		} `json:"data"`
	} `json:"stats"`
	Node struct {
		Callsign string `json:"callsign"`
	} `json:"node"`
}

// FetchNodeStatus queries baseURL (DefaultBaseURL if empty) for node's
// current status. Returns ErrNotFound for a 404 (see its own doc
// comment) rather than treating it as a transport failure.
func FetchNodeStatus(ctx context.Context, baseURL, node string) (*NodeStatus, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	reqURL := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(node)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("allstarapi: build request for node %q: %w", node, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("allstarapi: fetch node %q: %w", node, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("allstarapi: node %q: unexpected response %s", node, resp.Status)
	}

	var raw rawResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("allstarapi: decode node %q: %w", node, err)
	}

	status := &NodeStatus{
		Node:     node,
		Callsign: raw.Node.Callsign,
		Keyed:    raw.Stats.Data.Keyed,
	}
	byNumber := make(map[string]rawLinkedNode, len(raw.Stats.Data.LinkedNodes))
	for _, ln := range raw.Stats.Data.LinkedNodes {
		byNumber[string(ln.Name)] = ln
	}
	for _, num := range raw.Stats.Data.Links {
		p := Peer{Number: num}
		if ln, ok := byNumber[num]; ok {
			p.Callsign = ln.Callsign
			p.Status = ln.Status
			p.Location = ln.Server.Location
		}
		status.Peers = append(status.Peers, p)
	}
	return status, nil
}
