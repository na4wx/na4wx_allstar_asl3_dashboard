package server

import (
	"net/http"
)

// nodeSearchLimit bounds one search's results -- a short pick-list for
// an operator to scan and click, not a directory dump. AllStarLink's
// own published directory has well over 100,000 entries; a query
// matching thousands of them is exactly the case this caps.
const nodeSearchLimit = 10

type nodeSearchResult struct {
	Number      string `json:"number"`
	Callsign    string `json:"callsign"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
}

// handleNodeSearch answers the Commands tab's "connect by callsign"
// lookup (see the Connect/disconnect card's own callsign field in
// node_edit.html) -- the reverse of the number->callsign lookup used
// everywhere else in this app. Reads from the same local node
// directory (s.nodes, see internal/nodedb) that already backs those
// forward lookups, so results are only as fresh as the last daily
// refresh -- fine for "find a node to connect to", which doesn't need
// live data.
func (s *Server) handleNodeSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	entries := s.nodes.Search(q, nodeSearchLimit)
	results := make([]nodeSearchResult, len(entries))
	for i, e := range entries {
		results[i] = nodeSearchResult{
			Number:      e.Number,
			Callsign:    e.Callsign,
			Description: e.Description,
			Location:    e.Location,
		}
	}
	writeJSON(w, results)
}
