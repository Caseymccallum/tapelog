//go:build !linux

package sandbox

// restrictPlatform is unreachable off Linux (Restrict handles it).
func (o Options) restrictPlatform() error { return ErrUnsupported }
