package asteriskconf

import (
	"os"
	"strings"
	"testing"
)

func TestCreateSectionAppendsAtEOF(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2

[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	if err := CreateSection(path, "2000", []string{"node-main"}, []Pair{
		{Key: "rxchannel", Value: "SimpleUSB/2000", Op: "="},
		{Key: "duplex", Value: "1", Op: "="},
	}); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Resolve("2000")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Value("rxchannel"); got != "SimpleUSB/2000" {
		t.Errorf("rxchannel = %q", got)
	}
	if got, _ := r.Value("duplex"); got != "1" {
		t.Errorf("duplex = %q, want \"1\" (own override, not node-main's \"2\")", got)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(got), "[2000]") < strings.Index(string(got), "[1999]") {
		t.Error("new section should be appended after the existing one, not before")
	}
}

func TestCreateSectionAlreadyExistsErrors(t *testing.T) {
	path := writeTempConf(t, `[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	if err := CreateSection(path, "1999", []string{"node-main"}, nil); err == nil {
		t.Error("expected an error creating a section that already exists")
	}
}

func TestCreateSectionDoesNotDisturbExistingSections(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2
rxchannel = Local/pseudo

[1999](node-main)
rxchannel = SimpleUSB/1999
`)
	if err := CreateSection(path, "2000", []string{"node-main"}, []Pair{{Key: "rxchannel", Value: "SimpleUSB/2000", Op: "="}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "rxchannel = SimpleUSB/1999") {
		t.Errorf("existing node's own section must be untouched:\n%s", got)
	}
	if !strings.Contains(string(got), "rxchannel = Local/pseudo") {
		t.Errorf("node-main template must be untouched:\n%s", got)
	}
}

func TestRemoveSectionDeletesHeaderAndBody(t *testing.T) {
	path := writeTempConf(t, `[node-main](!)
duplex = 2

[1999](node-main)
rxchannel = SimpleUSB/1999

[2000](node-main)
rxchannel = SimpleUSB/2000
`)
	if err := RemoveSection(path, "1999"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "1999") {
		t.Errorf("all trace of the removed section should be gone:\n%s", s)
	}
	if !strings.Contains(s, "[2000]") || !strings.Contains(s, "rxchannel = SimpleUSB/2000") {
		t.Errorf("other sections must survive:\n%s", s)
	}
}

func TestRemoveSectionMissingIsNoop(t *testing.T) {
	const orig = "[1999](node-main)\nrxchannel = SimpleUSB/1999\n"
	path := writeTempConf(t, orig)
	if err := RemoveSection(path, "9999"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("file should be unchanged:\ngot:\n%q\nwant:\n%q", got, orig)
	}
}

func TestRemoveValueDeletesKeyFromSection(t *testing.T) {
	path := writeTempConf(t, `[nodes]
1998 = radio@127.0.0.1/1998,NONE
1999 = radio@127.0.0.1/1999,NONE
`)
	if err := RemoveValue(path, "nodes", "1999"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[nodes]\n1998 = radio@127.0.0.1/1998,NONE\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRemoveValueMissingKeyIsNoop(t *testing.T) {
	const orig = "[nodes]\n1998 = radio@127.0.0.1/1998,NONE\n"
	path := writeTempConf(t, orig)
	if err := RemoveValue(path, "nodes", "9999"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("file should be unchanged, got:\n%q", got)
	}
}
