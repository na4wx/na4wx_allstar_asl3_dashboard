package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hamvoipconfiggui-asl3/internal/sa818"
)

// ctcssOption is one entry in the CTCSS dropdowns: the module's own
// 4-digit code paired with the Hz value it's labeled with, matching
// what the module's own manual documents as its Tx_CTCSS/Rx_CTCSS index.
type ctcssOption struct {
	Code string
	Hz   string
}

func ctcssOptions() []ctcssOption {
	opts := make([]ctcssOption, len(sa818.CTCSSTones))
	for i, hz := range sa818.CTCSSTones {
		opts[i] = ctcssOption{Code: sa818.CTCSSCode(hz), Hz: hz}
	}
	return opts
}

// sa818SettingsFromForm builds a sa818.Settings from the submitted form:
// frequencies need all four decimal places the underlying tool's own
// prompt asks for (xxx.xxxx), and CTCSS fields need an explicit "0000"
// rather than being left blank when no tone is wanted. Returns a
// non-empty error string if the input can't be used.
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
	if s.TxCTCSS == "" {
		s.TxCTCSS = "0000"
	}
	if !sa818.ValidCTCSSCode(s.TxCTCSS) {
		return s, "Transmit CTCSS: not a value from the tone list"
	}
	s.RxCTCSS = strings.TrimSpace(r.FormValue("rx_ctcss"))
	if s.RxCTCSS == "" {
		s.RxCTCSS = "0000"
	}
	if !sa818.ValidCTCSSCode(s.RxCTCSS) {
		return s, "Receive CTCSS: not a value from the tone list"
	}

	squelch, err := strconv.Atoi(r.FormValue("squelch"))
	if err != nil || squelch < 1 || squelch > 9 {
		return s, "Squelch must be a number from 1 to 9"
	}
	s.Squelch = squelch

	volume, err := strconv.Atoi(r.FormValue("volume"))
	if err != nil || volume < 0 || volume > 8 {
		return s, "Volume must be a number from 0 to 8"
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

func (s *Server) handleSystemSA818Apply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	settings, ferr := sa818SettingsFromForm(r)
	if ferr != "" {
		s.renderSystemPage(w, r, flash("error", ferr))
		return
	}

	output, ok, err := sa818.Program(r.Context(), s.sa818Tool, settings)

	if s.sa818StatePath != "" {
		last := &sa818.LastApplied{
			Settings:  settings,
			Tool:      s.sa818Tool,
			AppliedAt: time.Now(),
			Success:   ok,
			Output:    output,
		}
		_ = sa818.SaveLast(s.sa818StatePath, last)
	}

	if err != nil {
		s.renderSystemPage(w, r, flash("error", "Could not run "+s.sa818Tool+": "+err.Error()))
		return
	}
	if !ok {
		s.renderSystemPage(w, r, flash("error", "The radio module rejected these settings — see the raw tool output below the form for details."))
		return
	}
	s.renderSystemPage(w, r, flash("ok", "Sent to the radio module — see the raw tool output below the form to confirm."))
}
