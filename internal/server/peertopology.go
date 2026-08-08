package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"hamvoipconfiggui-asl3/internal/allstarapi"
)

// peerTopologyFetchTimeout bounds one stats.allstarlink.org request --
// generous since it's a one-shot lookup triggered by an operator
// clicking a row's own button, not a poll.
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

// handlePeerStatus answers one row's own "Who's connected to them?"
// button on the "Connected right now" card: looks up peer (an
// arbitrary AllStarLink node number -- not necessarily one hosted on
// this machine) against stats.allstarlink.org and returns what it's
// currently linked to. This is the only way this app has to see a
// REMOTE node's own connections, since nothing about that node runs on
// this machine's own Asterisk.
func (s *Server) handlePeerStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), peerTopologyFetchTimeout)
	defer cancel()
	res := s.fetchPeerTopology(ctx, r.PathValue("peer"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// fetchPeerTopology resolves one node's own linked nodes. Directory
// (s.nodes) callsigns fill in wherever AllStarLink's own registration
// data doesn't have one -- confirmed common for both the queried node
// itself and its own peers (see allstarapi's package doc). Status
// reporting is opt-in on the queried node's own end, so a node that
// doesn't publish it (common) comes back as res.OK == false with a
// message rather than an error.
func (s *Server) fetchPeerTopology(ctx context.Context, peerNum string) peerTopologyResult {
	res := peerTopologyResult{Number: peerNum}
	if entry, ok := s.nodes.Lookup(peerNum); ok {
		res.Callsign = entry.Label()
	}

	status, err := allstarapi.FetchNodeStatus(ctx, s.aslStatsBaseURL, peerNum)
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
