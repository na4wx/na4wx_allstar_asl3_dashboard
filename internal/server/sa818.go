package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/sa818"
)

// ctcssOption is one entry in the CTCSS dropdowns. Unlike HamVoIP's
// version of this file (which paired a Hz value with 818-prog's own
// 4-digit module code), ASL3's sa818 tool takes the Hz value itself
// directly as its --ctcss argument -- so Code and Hz are the same
// string here; Code exists only so the template (which submits
// "value={{.Code}}") doesn't need to change.
type ctcssOption struct {
	Code string
	Hz   string
}

func ctcssOptions() []ctcssOption {
	opts := make([]ctcssOption, len(sa818.CTCSSTones))
	for i, hz := range sa818.CTCSSTones {
		opts[i] = ctcssOption{Code: hz, Hz: hz}
	}
	return opts
}

// sa818SettingsFromForm builds a sa818.Settings from the submitted form:
// frequencies need all four decimal places sa818's own "--frequency"
// flag expects (xxx.xxxx). Returns a non-empty error string if the input
// can't be used.
func sa818SettingsFromForm(r *http.Request) (sa818.Settings, string) {
	var s sa818.Settings
	s.Wide = r.FormValue("wide") == "1"

	txFreq, err := formatSA818Freq(r.FormValue("tx_freq"))
	if err != nil {
		return s, "Transmit frequency: " + err.Error()
	}
	s.TxFreqMHz = txFreq

	rxFreqInput := r.FormValue("rx_freq")
	if r.FormValue("same_freq") == "1" || strings.TrimSpace(rxFreqInput) == "" {
		s.RxFreqMHz = txFreq
	} else {
		rxFreq, err := formatSA818Freq(rxFreqInput)
		if err != nil {
			return s, "Receive frequency: " + err.Error()
		}
		s.RxFreqMHz = rxFreq
	}

	s.TxCTCSS = strings.TrimSpace(r.FormValue("tx_ctcss"))
	if !sa818.ValidCTCSSHz(s.TxCTCSS) {
		return s, "Transmit CTCSS: not a value from the tone list"
	}
	s.RxCTCSS = strings.TrimSpace(r.FormValue("rx_ctcss"))
	if !sa818.ValidCTCSSHz(s.RxCTCSS) {
		return s, "Receive CTCSS: not a value from the tone list"
	}

	// Squelch 0-8 and volume 1-8 are sa818's own real ranges, confirmed
	// via `sa818 radio --help`/`sa818 volume --help` on a live ASL3
	// node -- NOT 818-prog's ranges (squelch 1-9, volume 0-8), which
	// this form used before that was confirmed.
	squelch, err := strconv.Atoi(r.FormValue("squelch"))
	if err != nil || squelch < 0 || squelch > 8 {
		return s, "Squelch must be a number from 0 to 8"
	}
	s.Squelch = squelch

	volume, err := strconv.Atoi(r.FormValue("volume"))
	if err != nil || volume < 1 || volume > 8 {
		return s, "Volume must be a number from 1 to 8"
	}
	s.Volume = volume

	s.PreDeEmphasis = r.FormValue("pre_de_emphasis") == "1"
	s.HighPassFilter = r.FormValue("high_pass") == "1"
	s.LowPassFilter = r.FormValue("low_pass") == "1"

	return s, ""
}

// formatSA818Freq validates a user-entered frequency and pads it to the
// exact "xxx.xxxx" (4 decimal place) format the programmer tool's own
// prompt asks for.
func formatSA818Freq(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return "", err
	}
	return strconv.FormatFloat(f, 'f', 4, 64), nil
}

func (s *Server) handleNodeSA818Apply(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	settings, ferr := sa818SettingsFromForm(r)
	if ferr != "" {
		s.renderNodeEditErrorReq(w, r, num, ferr)
		return
	}

	output, ok, err := sa818.Program(r.Context(), s.sa818Port, settings)

	if s.sa818StatePath != "" {
		last := &sa818.LastApplied{
			Settings:  settings,
			Port:      s.sa818Port,
			AppliedAt: time.Now(),
			Success:   ok,
			Output:    output,
		}
		_ = sa818.SaveLast(s.sa818StatePath, last)
	}

	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Could not reach the radio module: "+err.Error())
		return
	}
	if !ok {
		s.renderNodeEditErrorReq(w, r, num, "The radio module rejected these settings — see the raw transcript below the form for details.")
		return
	}
	data, err := s.loadNodeEditData(num, flash("ok", "Sent to the radio module — see the raw transcript below the form to confirm."))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}
