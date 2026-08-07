// Sounds tab: shared (node-agnostic) custom sound file upload/delete,
// stock+custom listing, and "Create from text" generation via
// internal/tts (Piper first, espeak-ng as a same-page fallback if Piper
// can't run on this system). Reached from a node's own page since
// that's where an operator actually needs a sound, even though the
// underlying files aren't tied to any one node -- ported from the
// original HamVoIP app's internal/server/telemetry.go, trimmed to just
// the sound-library/TTS pieces (that file's courtesy-tone editing is a
// separate, not-yet-ported feature).
package server

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"hamvoipconfiggui-asl3/internal/tts"
)

// soundUploadMaxBytes bounds one upload -- generous for a short voice
// recording as an uncompressed WAV, small enough that one request can't
// meaningfully fill the disk.
const soundUploadMaxBytes = 20 << 20

// defaultTTSVoiceName is pre-selected in the "Create from text" voice
// dropdown when it's among the voices actually installed (see
// install.sh's own PIPER_DEFAULT_VOICES, which downloads this one among
// others) -- falls back to the browser's own "first option" default if
// it isn't installed, rather than erroring.
const defaultTTSVoiceName = "en_US-hfc_female-medium"

const (
	espeakFallbackEngine = "espeak"
	espeakNGTool         = "espeak-ng"
	espeakTool           = "espeak"
)

func formatTTSError(prefix string, output string) string {
	msg := prefix
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		msg += " — " + trimmed
	}
	return msg
}

func findVoiceByName(voices []tts.Voice, name string) (tts.Voice, bool) {
	for _, v := range voices {
		if v.Name == name {
			return v, true
		}
	}
	return tts.Voice{}, false
}

func (s *Server) resolveESpeakTool(ctx context.Context) (tool string, errMsg string) {
	if out, err := tts.CheckTool(ctx, espeakNGTool, "--version"); err == nil {
		return espeakNGTool, ""
	} else if _, err := tts.CheckTool(ctx, espeakTool, "--version"); err == nil {
		return espeakTool, ""
	} else {
		return "", formatTTSError("Text-to-speech is unavailable because neither espeak-ng nor espeak could start", out)
	}
}

// resolveTTSBackend chooses which engine this request/page should use:
// Piper first when healthy and voices exist, otherwise espeak-ng.
func (s *Server) resolveTTSBackend(ctx context.Context) (engine string, voices []tts.Voice, note string, errMsg string) {
	var piperErr string
	if piperVoices, err := tts.ListVoices(s.ttsVoicesDir); err == nil && len(piperVoices) > 0 {
		if output, checkErr := tts.CheckTool(ctx, s.ttsTool, "--help"); checkErr == nil {
			return "piper", piperVoices, "", ""
		} else {
			piperErr = formatTTSError("Piper couldn't start", output)
		}
	}

	espeakRuntimeTool, espeakErrMsg := s.resolveESpeakTool(ctx)
	if espeakErrMsg == "" {
		espeakVoices, espeakVoiceOut, espeakVoicesErr := tts.ListESpeakVoices(ctx, espeakRuntimeTool)
		if espeakVoicesErr == nil {
			note := ""
			if piperErr != "" {
				note = "Using espeak fallback because Piper is unavailable on this system."
			}
			return espeakFallbackEngine, espeakVoices, note, ""
		}
		return "", nil, "", formatTTSError("Text-to-speech is unavailable because espeak-ng voices could not be listed", espeakVoiceOut)
	}

	if piperErr != "" {
		return "", nil, "", "Text-to-speech is unavailable. " + piperErr + " " + espeakErrMsg
	}
	return "", nil, "", espeakErrMsg
}

func (s *Server) synthesizeWithEngine(ctx context.Context, engine, voiceName, text string) (wav []byte, output string, userErr string, err error) {
	switch engine {
	case "piper":
		voice, ok, findErr := tts.FindVoice(s.ttsVoicesDir, voiceName)
		if findErr != nil {
			return nil, "", "", findErr
		}
		if !ok {
			return nil, "", "Pick a voice — none selected, or it's no longer available", nil
		}
		if checkOut, checkErr := tts.CheckTool(ctx, s.ttsTool, "--help"); checkErr != nil {
			return nil, "", formatTTSError("Text-to-speech is unavailable because Piper couldn't start", checkOut), nil
		}
		wav, output, err = tts.Synthesize(ctx, s.ttsTool, voice.ModelPath, text)
		return wav, output, "", err

	case espeakFallbackEngine:
		espeakRuntimeTool, espeakErrMsg := s.resolveESpeakTool(ctx)
		if espeakErrMsg != "" {
			return nil, "", espeakErrMsg, nil
		}
		espeakVoices, espeakVoiceOut, listErr := tts.ListESpeakVoices(ctx, espeakRuntimeTool)
		if listErr != nil {
			return nil, "", formatTTSError("Text-to-speech is unavailable because espeak-ng voices could not be listed", espeakVoiceOut), nil
		}
		voice, ok := findVoiceByName(espeakVoices, voiceName)
		if !ok {
			return nil, "", "Pick a voice — none selected, or it's no longer available", nil
		}
		wav, output, err = tts.SynthesizeESpeak(ctx, espeakRuntimeTool, voice.ModelPath, text)
		return wav, output, "", err

	default:
		resolvedEngine, voices, _, msg := s.resolveTTSBackend(ctx)
		if msg != "" {
			return nil, "", msg, nil
		}
		if resolvedEngine == espeakFallbackEngine && len(voices) > 0 && voiceName == "" {
			voiceName = voices[0].Name
		}
		return s.synthesizeWithEngine(ctx, resolvedEngine, voiceName, text)
	}
}

// populateNodeSounds fills nodeEditData's Sounds-tab fields. Best-effort,
// like the rest of this page's supplementary data -- a read failure just
// leaves the section looking empty rather than failing the whole page.
func (s *Server) populateNodeSounds(data *nodeEditData) {
	if files, err := s.sounds.ListAll(); err == nil {
		data.SoundFiles = files
	}
	engine, voices, note, errMsg := s.resolveTTSBackend(context.Background())
	data.TTSEngine = engine
	data.TTSVoices = voices
	data.TTSNotice = note
	data.TTSError = errMsg
	data.TTSDefaultVoice = defaultTTSVoiceName
}

// handleNodeSoundUpload handles an uploaded audio file (typically a
// WAV), transcodes it via sox, and saves it as a custom sound the
// operator can then pick for idrecording or any telemetry entry.
func (s *Server) handleNodeSoundUpload(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseMultipartForm(soundUploadMaxBytes); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Upload too large or malformed: "+err.Error())
		return
	}
	name := strings.TrimSpace(r.FormValue("sound_name"))
	if name == "" {
		s.renderNodeEditErrorReq(w, r, num, "Enter a name for this sound file")
		return
	}
	file, _, err := r.FormFile("sound_file")
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Choose an audio file to upload")
		return
	}
	defer file.Close()

	if _, err := s.sounds.Upload(r.Context(), name, file); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Couldn't convert the uploaded file: "+err.Error())
		return
	}
	data, err := s.loadNodeEditData(r.Context(), num, flash("ok", "Uploaded \""+name+"\" — pick it from any sound field."))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}

// handleNodeSoundTTS generates a custom sound file from typed text using
// whichever TTS backend is active (Piper first, espeak-ng fallback),
// then saves it through the exact same sounds.Store.Upload path a
// manual upload uses. Voice names are always resolved by listing the
// backend's own available voices first; they are never used to build
// file paths directly from form input.
func (s *Server) handleNodeSoundTTS(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("sound_name"))
	if name == "" {
		s.renderNodeEditErrorReq(w, r, num, "Enter a name for this sound file")
		return
	}
	text := strings.TrimSpace(r.FormValue("tts_text"))
	if text == "" {
		s.renderNodeEditErrorReq(w, r, num, "Enter the text to speak")
		return
	}
	voiceName := strings.TrimSpace(r.FormValue("tts_voice"))
	engine := strings.TrimSpace(r.FormValue("tts_engine"))

	wav, output, userErr, err := s.synthesizeWithEngine(r.Context(), engine, voiceName, text)
	if userErr != "" {
		s.renderNodeEditErrorReq(w, r, num, userErr)
		return
	}
	if err != nil {
		s.renderNodeEditErrorReq(w, r, num, formatTTSError("Couldn't generate speech: "+err.Error(), output))
		return
	}
	if _, err := s.sounds.Upload(r.Context(), name, bytes.NewReader(wav)); err != nil {
		s.renderNodeEditErrorReq(w, r, num, "Generated the audio, but couldn't convert it: "+err.Error())
		return
	}
	data, err := s.loadNodeEditData(r.Context(), num, flash("ok", "Generated \""+name+"\" from text — pick it from any sound field."))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}

// handleNodeSoundTTSPreview synthesizes speech for the submitted
// voice+text and returns it directly as WAV bytes -- no sox transcoding,
// no save. Called from the "Preview" button via fetch(), not a normal
// form submission, so errors go back as a plain text body/status code
// for the page's own JS to show, not a flash + full page re-render.
func (s *Server) handleNodeSoundTTSPreview(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(r.FormValue("tts_text"))
	if text == "" {
		http.Error(w, "Enter the text to speak", http.StatusBadRequest)
		return
	}
	voiceName := strings.TrimSpace(r.FormValue("tts_voice"))
	engine := strings.TrimSpace(r.FormValue("tts_engine"))

	wav, output, userErr, err := s.synthesizeWithEngine(r.Context(), engine, voiceName, text)
	if userErr != "" {
		http.Error(w, userErr, http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		http.Error(w, formatTTSError(err.Error(), output), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Write(wav)
}

// handleNodeSoundAudio streams one of the operator's own custom sounds,
// transcoded on the fly to browser-playable WAV, for the "Play" button
// next to each row in the custom sound files table. Never reachable for
// a stock library file -- same boundary as handleNodeSoundDelete.
func (s *Server) handleNodeSoundAudio(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	wav, err := s.sounds.Preview(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Write(wav)
}

// handleNodeSoundDelete removes one of the operator's own custom sound
// files. Never reachable for a stock library file -- see
// sounds.Store.DeleteCustom, which only ever touches the custom
// directory.
func (s *Server) handleNodeSoundDelete(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("node")
	if _, err := s.cfg.LoadNode(num); err != nil {
		http.NotFound(w, r)
		return
	}
	name := r.PathValue("name")
	if err := s.sounds.DeleteCustom(name); err != nil {
		s.renderNodeEditErrorReq(w, r, num, err.Error())
		return
	}
	data, err := s.loadNodeEditData(r.Context(), num, flash("ok", "Deleted sound \""+name+"\"."))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "node_edit.html", data)
}
