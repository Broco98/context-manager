package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestEmitJSONWritesData(t *testing.T) {
	var buf bytes.Buffer
	out = &buf // package-level writer override for tests
	if err := Emit(true, "ignored human text", map[string]any{"task": "add-payment"}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["task"] != "add-payment" {
		t.Errorf("got task=%v, want add-payment", got["task"])
	}
}

func TestEmitTextWritesHuman(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	if err := Emit(false, "hello human", map[string]any{"task": "x"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello human\n" {
		t.Errorf("got %q, want %q", buf.String(), "hello human\n")
	}
}

func TestErrorfJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	err := Errorf(true, ErrNotFound, "task %q not found", "x")
	if err == nil {
		t.Fatal("Errorf should return a non-nil error")
	}
	var got map[string]string
	if jerr := json.Unmarshal(buf.Bytes(), &got); jerr != nil {
		t.Fatalf("not JSON: %v\n%s", jerr, buf.String())
	}
	if got["code"] != "NOT_FOUND" || got["error"] == "" {
		t.Errorf("bad envelope: %v", got)
	}
}

func TestErrorfMarksRenderedAndRenderSkipsDouble(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	err := Errorf(true, ErrUsage, "bad input")
	if !IsRendered(err) {
		t.Fatal("Errorf result should be marked rendered")
	}
	buf.Reset()
	if got := Render(true, err); got == nil {
		t.Fatal("Render should pass the error through")
	}
	if buf.Len() != 0 {
		t.Errorf("Render should not re-emit an already-rendered error, got %q", buf.String())
	}
}

func TestRenderEmitsEnvelopeForRawError(t *testing.T) {
	var buf bytes.Buffer
	out = &buf
	_ = Render(true, os.ErrNotExist)
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got["code"] != "NOT_FOUND" || got["error"] == "" {
		t.Errorf("bad raw envelope: %v", got)
	}
}
