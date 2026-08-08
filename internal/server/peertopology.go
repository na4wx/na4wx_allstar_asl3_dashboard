package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"hamvoipconfiggui-asl3/internal/allstarapi"
	"hamvoipconfiggui-asl3/internal/rptstatus"
	"hamvoipconfiggui-asl3/internal/system"
)

// peerTopologyFetchTimeout bounds one stats.allstarlink.org request --
// generous since it's a one-shot lookup triggered by an operator opening
// the modal, not a poll, but still bounded so one slow/hanging peer
// can't stall the whole response.
const peerTopologyFetchTimeout = 8 * time.Second

type peerTopologyPeer struct {
	Number   string `json:"number"`
	Callsign string `json:"callsign,omitempty"`
	Status   string `json:"status,omitempty"`
	Location string `json:"location,omitempty"`
}

type peerTopologyResult struct {
	Number      string             `json:"number"`
	Callsign    string             `json:"callsign,omitempty"`
	OK          bool               `json:"ok"`
	Error       string             `json:"error,omitempty"`
	Keyed       bool               `json:"keyed"`
	ConnectedTo []peerTopologyPeer `json:"connectedTo,omitempty"`
}

// handleNodePeerTopology answers the "Connected right now" card's own
// "Who else are they connected to?" button: for each peer currently
// linked to node, asks stats.allstarlink.org what THAT peer is in turn
// linked to -- a second-hop view this app has no other way to get,
// since those are other operators' nodes, not ones running on this
// machine's own Asterisk. See internal/allstarapi's package doc for why
// a peer commonly comes back with no data at all (status reporting is
// opt-in on the peer's own end, not something this app controls).
//
// Peers are looked up concurrently -- an operator's connected set is
// usually small, but each lookup is its own outbound HTTPS round trip,
// and there's no reason to make them wait on each other.
func (s *Server) handleNodePeerTopology(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	activityOut, err := system.AsteriskRX(ctx, s.asteriskBin, "rpt lstats "+num)
	if err != nil {
		http.Error(w, "could not read connected peers: "+err.Error(), http.StatusBadGateway)
		return
	}

	var peerNumbers []string
	if _, rows, ok := rptstatus.ParseLstats(activityOut); ok {
		for _, row := range rows {
			if len(row) > 0 && row[0] != "" {
				peerNumbers = append(peerNumbers, row[0])
			}
		}
	}

	results := make([]peerTopologyResult, len(peerNumbers))
	var wg sync.WaitGroup
	for i, peerNum := range peerNumbers {
		wg.Add(1)
		go func(i int, peerNum string) {
			defer wg.Done()
			results[i] = s.fetchPeerTopology(ctx, peerNum)
		}(i, peerNum)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"node":  num,
		"peers": results,
	})
}

// fetchPeerTopology resolves one connected peer's own linked nodes.
// Directory (s.nodes) callsigns fill in wherever AllStarLink's own
// registration data doesn't have one -- confirmed common for both the
// peer itself and its own peers (see allstarapi's package doc).
func (s *Server) fetchPeerTopology(ctx context.Context, peerNum string) peerTopologyResult {
	res := peerTopologyResult{Number: peerNum}
	if entry, ok := s.nodes.Lookup(peerNum); ok {
		res.Callsign = entry.Label()
	}

	fctx, cancel := context.WithTimeout(ctx, peerTopologyFetchTimeout)
	defer cancel()
	status, err := allstarapi.FetchNodeStatus(fctx, s.aslStatsBaseURL, peerNum)
	if err != nil {
		res.OK = false
		if errors.Is(err, allstarapi.ErrNotFound) {
			res.Error = "This node has never reported live status to AllStarLink (status reporting is off, or it's not an AllStarLink node)."
		} else {
			res.Error = "Couldn't reach AllStarLink's status service."
		}
		return res
	}

	res.OK = true
	res.Keyed = status.Keyed
	if status.Callsign != "" {
		res.Callsign = status.Callsign
	}
	for _, p := range status.Peers {
		callsign := p.Callsign
		if callsign == "" {
			if entry, ok := s.nodes.Lookup(p.Number); ok {
				callsign = entry.Label()
			}
		}
		res.ConnectedTo = append(res.ConnectedTo, peerTopologyPeer{
			Number:   p.Number,
			Callsign: callsign,
			Status:   p.Status,
			Location: p.Location,
		})
	}
	return res
}
