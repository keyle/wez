package main

import "testing"

func TestParseCLIURLOnly(t *testing.T) {
	opts, err := parseCLI([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.url != "https://example.com" {
		t.Fatalf("unexpected URL: %q", opts.url)
	}
	if opts.dumpMode {
		t.Fatal("dumpMode should be false")
	}
}

func TestParseCLIDump(t *testing.T) {
	opts, err := parseCLI([]string{"--dump", "example.com"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if !opts.dumpMode {
		t.Fatal("expected dumpMode true")
	}
	if opts.url != "example.com" {
		t.Fatalf("unexpected URL: %q", opts.url)
	}
}

func TestParseCLIDumpMissingURL(t *testing.T) {
	_, err := parseCLI([]string{"--dump"})
	if err == nil {
		t.Fatal("expected --dump missing URL error")
	}
}

func TestParseCLIUnknownFlag(t *testing.T) {
	_, err := parseCLI([]string{"--nope"})
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
}
