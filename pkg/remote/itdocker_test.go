//go:build sshd_integration

package remote_test

// The harness of the sshd lane (#239). scripts/remote-it.sh builds the
// gitbox-sshd image and the linux/amd64 server with the e2e_sentinel tag,
// then runs:
//
//	go test -race -tags sshd_integration ./pkg/remote/
//
// Each test starts its own container on an --internal Docker network (no
// egress), reached by its IP, and talks to it through the real system ssh
// with a config file of its own. ithost_test.go is the same harness for a
// real host.

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

var dockerIT struct {
	image, network string
	pubKey         string
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "sshd integration: "+format+"\n", args...)
		return 1
	}
	dockerIT.image = os.Getenv("GITBOX_IT_IMAGE")
	if dockerIT.image == "" {
		dockerIT.image = "gitbox-sshd"
	}
	if err := loadServer("linux/amd64"); err != nil {
		return fail("%v; run scripts/remote-it.sh", err)
	}

	// Control sockets need a short path.
	var err error
	if it.keyDir, err = os.MkdirTemp("/tmp", "gbxit"); err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(it.keyDir)
	key := filepath.Join(it.keyDir, "id")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "gitbox-it", "-f", key).CombinedOutput(); err != nil {
		return fail("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return fail("%v", err)
	}
	dockerIT.pubKey = strings.TrimSpace(string(pub))

	dockerIT.network = "gbx-it-" + strconv.Itoa(os.Getpid())
	if out, err := exec.Command("docker", "network", "create", "--internal", dockerIT.network).CombinedOutput(); err != nil {
		return fail("docker network create: %v %s", err, out)
	}
	defer func() { _ = exec.Command("docker", "network", "rm", dockerIT.network).Run() }()
	return m.Run()
}

type container struct {
	t  *testing.T
	id string
	ip string
}

func startContainer(t *testing.T, o ctOpts) *container {
	t.Helper()
	args := []string{"run", "-d", "--rm", "--network", dockerIT.network, "-e", "AUTHORIZED_KEY=" + dockerIT.pubKey}
	if o.shell != "" {
		args = append(args, "-e", "LOGIN_SHELL="+o.shell)
	}
	if o.bashrc != "" {
		args = append(args, "-e", "BASHRC_B64="+base64.StdEncoding.EncodeToString([]byte(o.bashrc)))
	}
	if o.noexecHome {
		args = append(args, "--tmpfs", testHome+":noexec,uid=1000,gid=1000,mode=0755")
	}
	if o.netAdmin {
		args = append(args, "--cap-add", "NET_ADMIN")
	}
	if o.gitDaemon {
		args = append(args, "-e", "GIT_DAEMON=1")
	}
	if o.gitHost {
		args = append(args, "-e", "GIT_HOST=1")
	}
	c := &container{t: t, id: docker(t, append(args, dockerIT.image)...)}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", c.id).Run() })
	c.ip = docker(t, "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", c.id)
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.ip, "22"), time.Second)
		if err == nil {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, _ := bufio.NewReader(conn).ReadString('\n')
			_ = conn.Close()
			if strings.HasPrefix(line, "SSH-") {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd in %s did not answer: %v\n%s", c.ip, err, docker(t, "logs", c.id))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// sh runs a command in the container as the test user.
func (c *container) sh(script string) string {
	c.t.Helper()
	out, err := exec.Command("docker", "exec", "-u", "test", "-e", "HOME="+testHome, c.id, "sh", "-c", script).CombinedOutput()
	if err != nil {
		c.t.Fatalf("in container: %s: %v\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ok runs a command as the test user and reports whether it succeeded.
func (c *container) ok(script string) bool {
	return exec.Command("docker", "exec", "-u", "test", "-e", "HOME="+testHome, c.id, "sh", "-c", script).Run() == nil
}

func (c *container) exists(path string) bool {
	return exec.Command("docker", "exec", c.id, "test", "-e", path).Run() == nil
}

// writeFileAsTest writes content to path as the test user, through stdin,
// so it is on no command line.
func (c *container) writeFileAsTest(path, content string) {
	c.t.Helper()
	cmd := exec.Command("docker", "exec", "-i", "-u", "test", c.id, "sh", "-c", "umask 077 && cat > "+path)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.t.Fatalf("writing %s: %v\n%s", path, err, out)
	}
}

// install copies a program of this machine to path in the container.
func (c *container) install(local, path string) {
	c.t.Helper()
	docker(c.t, "cp", local, c.id+":"+path)
}

// background starts a command as the test user and leaves it running; it
// dies with the container.
func (c *container) background(script string) {
	c.t.Helper()
	docker(c.t, "exec", "-d", "-u", "test", c.id, "sh", "-c", script)
}

// sampleProcesses appends the command line of every process of the host
// to log every 50 ms. It runs as root, so it sees every process.
func (c *container) sampleProcesses(log string) {
	c.t.Helper()
	docker(c.t, "exec", "-d", c.id, "sh", "-c", "while :; do ps -eww -o args >> "+log+"; sleep 0.05; done")
}

func (c *container) home() string       { return testHome }
func (c *container) goos() string       { return "linux" }
func (c *container) scratch() string    { return "/tmp" }
func (c *container) serverDir() string  { return "" }
func (c *container) serverRoot() string { return testHome + "/.gitbox-server" }

// hostBlock is the body of the ssh config Host block that reaches the
// container.
func (c *container) hostBlock() string {
	return fmt.Sprintf("  HostName %s\n  User test\n  IdentityFile %s\n", c.ip, filepath.Join(it.keyDir, "id"))
}

// knownHosts is the known_hosts line of the container's host key.
func (c *container) knownHosts() string {
	return c.ip + " " + docker(c.t, "exec", c.id, "cat", "/etc/ssh/ssh_host_ed25519_key.pub") + "\n"
}

// requireNoEgress fails unless the container has no default route: the
// routing table has no 00000000 destination.
func (c *container) requireNoEgress() {
	c.t.Helper()
	for _, line := range strings.Split(c.sh("cat /proc/net/route"), "\n")[1:] {
		if f := strings.Fields(line); len(f) > 1 && f[1] == "00000000" {
			c.t.Fatalf("the container has a default route: %s", line)
		}
	}
}

// terminalShells are the login shells TestRemoteTerminalOverSSH runs in,
// each in a container of its own.
func terminalShells(*testing.T) []terminalShell {
	return []terminalShell{
		{"/bin/bash", remote.FamilyPOSIX},
		{"/usr/bin/fish", remote.FamilyFish},
		{"/bin/sh", remote.FamilyOther}, // the generic wrapper, read by dash
	}
}
