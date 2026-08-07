// Commands tab: a node's DTMF function map (which touch-tone digits run
// which app_rpt command), its saved macros (multi-step DTMF sequences
// run from one dial), a live snapshot of who's connected, and a way to
// send an arbitrary touch-tone sequence directly -- what used to be a
// standalone "Connections" page in the original HamVoIP app, folded
// into the node's own tabs here since it's scoped to exactly one node
// anyway.
package server

import (
	"context"
	"net/http"
	"strings"

	"hamvoipconfiggui-asl3/internal/system"
)

// populateNodeCommands fills nodeEditData's Commands-tab fields.
// Best-effort, like the rest of this page's supplementary data -- a
// read failure just leaves the section looking empty rather than
// failing the whole page.
func (s *Server) populateNodeCommands(ctx context.Context, data *nodeEditData) {
	if data.View == nil || data.View.Node == "" {
		return
	}
	view := data.View

	data.FunctionsSect = view.Functions
	if macros, err := s.cfg.ListFunctionMacros(view.Functions); err == nil {
		data.Macros = macros
	}

	data.MacroSect = view.Macro
	if defs, err := s.cfg.ListFunctionMacros(view.Macro); err == nil {
		data.MacroDefs = defs
	}

	if out, err := system.AsteriskRX(ctx, s.asteriskBin, "rpt nodes "+view.Node); err != nil {
		data.LinkStatusErr = err.Error()
	} else {
		data.LinkStatus = out
	}
}

func (s *Server) handleNodeMacroSave(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	digits := strings.TrimSpace(r.FormValue("digits"))
	command := strings.TrimSpace(r.FormValue("command"))
	if digits == "" || command == "" {
		http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
		return
	}
	if err := s.cfg.SetFunctionMacro(view.Functions, digits, command); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

func (s *Server) handleNodeMacroDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	digits := r.PathValue("digits")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.cfg.DeleteFunctionMacro(view.Functions, digits); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeMacroDefSave and handleNodeMacroDefDelete edit the node's
// macro stanza (its saved multi-step DTMF sequences, invoked via the
// "macro,<n>" function) -- a different rpt.conf section from the
// function/command map above, but structurally identical (digit key ->
// DTMF string), so they reuse the same Store methods against
// view.Macro instead of view.Functions.

func (s *Server) handleNodeMacroDefSave(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	digits := strings.TrimSpace(r.FormValue("digits"))
	sequence := strings.TrimSpace(r.FormValue("sequence"))
	if digits == "" || sequence == "" {
		http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
		return
	}
	if err := s.cfg.SetFunctionMacro(view.Macro, digits, sequence); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

func (s *Server) handleNodeMacroDefDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	digits := r.PathValue("digits")
	view, err := s.cfg.LoadNode(num)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.cfg.DeleteFunctionMacro(view.Macro, digits); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	http.Redirect(w, r, "/nodes/"+num, http.StatusSeeOther)
}

// handleNodeSendDTMF relays a literal DTMF command string to
// `asterisk -rx "rpt fun <node> <digits>"` -- i.e. exactly what would
// happen if that sequence were dialed on the radio. The digits are
// supplied by the operator, not inferred from the function map, since
// guessing at command syntax and sending it to a live repeater without
// hardware to verify against is not a risk worth taking; the command
// list on this same page tells them what to type.
func (s *Server) handleNodeSendDTMF(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	digits := strings.TrimSpace(r.FormValue("digits"))
	if digits == "" {
		s.renderNodeEditErrorReq(w, r, num, "Enter a DTMF sequence to send")
		return
	}

	out, err := system.AsteriskRX(r.Context(), s.asteriskBin, "rpt fun "+num+" "+digits)
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	msg := "Sent " + digits + " to node " + num
	if strings.TrimSpace(out) != "" {
		msg += ": " + strings.TrimSpace(out)
	}
	data, loadErr := s.loadNodeEditData(r.Context(), num, flash("ok", msg))
	if loadErr != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}
