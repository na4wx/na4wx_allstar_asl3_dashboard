package config

import "testing"

func TestIsCWIDValue(t *testing.T) {
	if !IsCWIDValue("|iNOTSET") {
		t.Error("expected |iNOTSET to be recognized as CW-ID syntax")
	}
	if IsCWIDValue("rpt/callproceeding") {
		t.Error("expected a plain sound reference to not be recognized as CW-ID syntax")
	}
}

func TestParseCWIDText(t *testing.T) {
	text, ok := ParseCWIDText("|iW1AW")
	if !ok || text != "W1AW" {
		t.Errorf("ParseCWIDText = %q, %v, want \"W1AW\", true", text, ok)
	}
	if _, ok := ParseCWIDText("rpt/callproceeding"); ok {
		t.Error("expected ok=false for a plain sound reference")
	}
}

func TestFormatCWID(t *testing.T) {
	if got := FormatCWID("W1AW"); got != "|iW1AW" {
		t.Errorf("FormatCWID = %q, want \"|iW1AW\"", got)
	}
}

// TestLoadNodePopulatesIDRecordingFromRealFixture confirms IDRecording
// resolves from the real node's own node-main template, matching its
// confirmed shipped placeholder value.
func TestLoadNodePopulatesIDRecordingFromRealFixture(t *testing.T) {
	view, err := testStore().LoadNode("1999")
	if err != nil {
		t.Fatal(err)
	}
	if view.IDRecording != "|iNOTSET" {
		t.Errorf("IDRecording = %q, want \"|iNOTSET\"", view.IDRecording)
	}
	text, ok := ParseCWIDText(view.IDRecording)
	if !ok || text != "NOTSET" {
		t.Errorf("ParseCWIDText(IDRecording) = %q, %v", text, ok)
	}
}
