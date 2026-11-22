package remote

import "io"

// The test-only hooks of Options. OpenSSH finds ~/.ssh through the password
// database, not $HOME, so a test cannot redirect it through the
// environment: it hands ssh its own config file instead, which sets its own
// known_hosts. Production code has no way to set these fields.

// SetConfigFile makes every ssh call read path with -F.
func SetConfigFile(o *Options, path string) { o.configFile = path }

// SetSSHPath replaces the system ssh.
func SetSSHPath(o *Options, path string) { o.sshPath = path }

// SetUploadFilter wraps the stream an install uploads.
func SetUploadFilter(o *Options, f func(io.Reader) io.Reader) { o.uploadFilter = f }
