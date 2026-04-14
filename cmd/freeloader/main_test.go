package main

import (
	"bytes"
	"testing"
)

func TestCLI_Mount(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"mount", "--path", "/tmp/test"})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Should not error (dry run with no real mount)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("mount command failed: %v", err)
	}
}

func TestCLI_ProviderAdd(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"provider", "list"})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("provider list failed: %v", err)
	}
}

func TestCLI_Status(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"status"})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("status command failed: %v", err)
	}
}

func TestCLI_Sync(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"sync"})
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("sync command failed: %v", err)
	}
}
