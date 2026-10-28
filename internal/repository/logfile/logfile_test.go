package logfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterRotates(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "hop.log", 10, 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	for _, line := range []string{"aaaaaa\n", "bbbbbb\n", "cccccc\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	for name, want := range map[string]string{"hop.log": "cccccc\n", "hop.log.1": "bbbbbb\n"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q (%v), want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hop.log.2")); err == nil {
		t.Fatal("kept more files than asked")
	}
}

func TestWarningsOnlyDropsInfoLines(t *testing.T) {
	var out bytes.Buffer
	f := NewWarningsOnly(&out)
	// One line arrives in two pieces, as it can from a pipe.
	for _, chunk := range []string{
		`time="t" level=info msg="[TCP] a --> b:443 using DIRECT"` + "\n" + `time="t" level=warn`,
		`ing msg="dial failed"` + "\n" + "panic: boom\n",
	} {
		if _, err := f.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	want := `time="t" level=warning msg="dial failed"` + "\npanic: boom\n"
	if out.String() != want {
		t.Fatalf("kept %q, want %q", out.String(), want)
	}
}
