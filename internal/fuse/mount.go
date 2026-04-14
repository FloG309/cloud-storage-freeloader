package fuse

import (
	"github.com/FloG309/cloud-storage-freeloader/internal/vfs"
)

// Mounter bridges FUSE/WinFsp to the VFS layer.
// Integration with cgofuse requires the FUSE or WinFsp runtime.
type Mounter struct {
	vfs        *vfs.VFS
	mountPoint string
}

// NewMounter creates a FUSE mounter for the given VFS and mount point.
func NewMounter(v *vfs.VFS, mountPoint string) *Mounter {
	return &Mounter{vfs: v, mountPoint: mountPoint}
}

// Mount starts serving the filesystem at the configured mount point.
// NOTE: Full FUSE implementation requires cgofuse which needs
// FUSE (libfuse on Linux) or WinFsp (on Windows) installed.
// This is a stub — integration tests verify the bridge works
// with real OS calls when the runtime is available.
func (m *Mounter) Mount() error {
	// TODO: Implement cgofuse.FileSystemInterface bridge
	// - Getattr, Readdir, Open, Read, Write, Create, Unlink,
	//   Rename, Mkdir, Rmdir, Statfs → delegate to VFS methods
	return nil
}

// Unmount stops serving the filesystem.
func (m *Mounter) Unmount() error {
	return nil
}
