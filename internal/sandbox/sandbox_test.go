package sandbox

import (
	"errors"
	"reflect"
	"testing"
)

func TestEnabled(t *testing.T) {
	if (Options{}).Enabled() {
		t.Error("empty options must not be enabled")
	}
	if !(Options{ReadOnly: []string{"/x"}}).Enabled() {
		t.Error("ro path must enable")
	}
	if !(Options{ReadWrite: []string{"/x"}}).Enabled() {
		t.Error("rw path must enable")
	}
}

func TestWrapCommand(t *testing.T) {
	o := Options{ReadOnly: []string{"/data"}, ReadWrite: []string{"/out"}, Lenient: true}
	got := o.WrapCommand("/usr/bin/cassette", []string{"node", "server.js"})
	want := []string{"/usr/bin/cassette", "__sandbox_exec", "--lenient", "--ro", "/data", "--rw", "/out", "--", "node", "server.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WrapCommand =\n%v\nwant\n%v", got, want)
	}

	plain := Options{ReadOnly: []string{"/data"}}
	got = plain.WrapCommand("cassette", []string{"srv"})
	want = []string{"cassette", "__sandbox_exec", "--ro", "/data", "--", "srv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WrapCommand =\n%v\nwant\n%v", got, want)
	}
}

func TestRestrictUnsupportedPlatform(t *testing.T) {
	if Available() {
		t.Skip("linux enforcement covered by restrict_linux_test.go")
	}
	err := (Options{ReadOnly: []string{"/x"}}).Restrict()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("strict mode must fail on unsupported OS, got %v", err)
	}
	if err := (Options{ReadOnly: []string{"/x"}, Lenient: true}).Restrict(); err != nil {
		t.Fatalf("lenient mode must proceed, got %v", err)
	}
}
