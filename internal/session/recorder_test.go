package session

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSessionRecorderStartStop(t *testing.T) {
	recorder := NewSessionRecorder("session-1")
	started := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := recorder.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := recorder.Start(started); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start() error = %v, want %v", err, ErrAlreadyStarted)
	}
	stopped := started.Add(time.Minute)
	if err := recorder.Stop(stopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := recorder.Stop(stopped); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	recording := recorder.Recording()
	if recording.StartedAt != started || recording.StoppedAt != stopped {
		t.Fatalf("recording times = %v, %v", recording.StartedAt, recording.StoppedAt)
	}
}

func TestSessionRecorderCapturesWrites(t *testing.T) {
	recorder := NewSessionRecorder("session-2")
	if err := recorder.WriteInput([]byte("early")); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("WriteInput() before Start error = %v", err)
	}
	if err := recorder.Start(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	input := []byte("echo hello\n")
	if err := recorder.WriteInput(input); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	if err := recorder.WriteOutput([]byte("hello\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Stop(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := recorder.WriteOutput([]byte("late")); !errors.Is(err, ErrStopped) {
		t.Fatalf("WriteOutput() after Stop error = %v", err)
	}
	events := recorder.Recording().Events
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Direction != DirectionInput || string(events[0].Data) != "echo hello\n" {
		t.Fatalf("input event = %#v", events[0])
	}
	if events[1].Direction != DirectionOutput || string(events[1].Data) != "hello\r\n" {
		t.Fatalf("output event = %#v", events[1])
	}
}

func TestSessionRecorderSerialization(t *testing.T) {
	recorder := NewSessionRecorder("session-3")
	started := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := recorder.Start(started); err != nil {
		t.Fatal(err)
	}
	if err := recorder.WriteOutput([]byte{0, 1, 2, 255}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Stop(started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	data, err := recorder.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	var recording Recording
	if err := json.Unmarshal(data, &recording); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if recording.SessionID != "session-3" || len(recording.Events) != 1 {
		t.Fatalf("recording = %#v", recording)
	}
	want := []byte{0, 1, 2, 255}
	if string(recording.Events[0].Data) != string(want) {
		t.Fatalf("event data = %v, want %v", recording.Events[0].Data, want)
	}
}
