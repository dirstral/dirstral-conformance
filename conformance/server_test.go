package conformance_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// server launches the binary under test and exposes its MCP endpoint.
//
// Black-box on purpose: nothing here knows anything about the implementation
// beyond the command line documented in its own --help and the NDJSON it
// promises on stdout. A second implementation satisfies this by accepting the
// same flags, which is the point of the harness existing at all.
type server struct {
	URL     string // full MCP endpoint, e.g. http://127.0.0.1:54541/mcp
	Corpus  string // the directory being served
	cmd     *exec.Cmd
	logMu   sync.Mutex
	logLine []string
}

// bootTimeout bounds the wait for the listening event. Generous: a cold start
// opens a store and loads an index, and a CI runner is slower than a laptop.
const bootTimeout = 90 * time.Second

// binaryPath returns the server under test, or "" when none was provided.
//
// DIR2MCP_BINARY is the documented knob. The name is historical and says
// nothing about which implementation it points at.
func binaryPath() string { return strings.TrimSpace(os.Getenv("DIR2MCP_BINARY")) }

// requireServer starts a server over the given corpus, or skips.
//
// Skipping without a binary is deliberate and is NOT a silent pass: the suite
// prints why, and `make conformance` in a release pipeline is expected to set
// the variable. See TestHarness_RefusesToPassVacuously.
func requireServer(t *testing.T, files map[string]string) *server {
	t.Helper()
	bin := binaryPath()
	if bin == "" {
		t.Skip("DIR2MCP_BINARY is unset: nothing to hold to the contract")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("DIR2MCP_BINARY=%q is not runnable: %v", bin, err)
	}

	root := t.TempDir()
	corpus := filepath.Join(root, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatalf("create corpus: %v", err)
	}
	for name, body := range files {
		path := filepath.Join(corpus, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// A config of our own, never the machine's. A harness that inherited the
	// operator's providers would pass or fail on their credentials rather than
	// on the server's conformance, and would not run in CI at all.
	config := filepath.Join(root, "conformance.yaml")
	if err := os.WriteFile(config, []byte("stt_provider: \"off\"\nrecognize_provider: \"off\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	srv := &server{Corpus: corpus}
	// --read-only keeps the run credential-free: ingestion needs an embedding
	// provider, the contracts asserted here do not. What that costs is stated
	// in the suite doc comment rather than hidden.
	srv.cmd = exec.Command(bin,
		"--config", config,
		"up",
		"--dir", corpus,
		"--state-dir", filepath.Join(root, "state"),
		"--foreground", "--json", "--non-interactive", "--read-only",
		"--auth", "none",
		"--listen", "127.0.0.1:0",
	)
	// Strip provider credentials from the child's environment for the same
	// reason as the config: the result must not depend on who is running it.
	srv.cmd.Env = scrubbedEnv()

	stdout, err := srv.cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	srv.cmd.Stderr = os.Stderr
	if err := srv.cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	t.Cleanup(func() { srv.stop() })

	url, err := srv.waitForListenAddr(stdout)
	if err != nil {
		t.Fatalf("%v\nserver output:\n  %s", err, strings.Join(srv.lines(), "\n  "))
	}
	srv.URL = url
	return srv
}

// credentialEnvPrefixes name the variables that would let an operator's
// account decide a conformance result.
var credentialEnvPrefixes = []string{
	"MISTRAL_", "OPENAI_", "GEMINI_", "GOOGLE_", "ANTHROPIC_", "COHERE_",
	"VOYAGE_", "ELEVENLABS_", "HF_", "HUGGINGFACE_", "AZURE_", "DIR2MCP_",
}

func scrubbedEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		drop := false
		for _, prefix := range credentialEnvPrefixes {
			if strings.HasPrefix(name, prefix) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// waitForListenAddr reads the NDJSON startup stream until the server says
// where it is listening.
func (s *server) waitForListenAddr(stdout interface{ Read([]byte) (int, error) }) (string, error) {
	type event struct {
		Event string `json:"event"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			URL        string `json:"url"`
			ListenAddr string `json:"listen_addr"`
		} `json:"data"`
	}

	found := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		announced := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			s.record(line)
			var ev event
			if json.Unmarshal([]byte(line), &ev) != nil {
				continue
			}
			if ev.Error != nil {
				select {
				case failed <- fmt.Errorf("server refused to start: %s: %s", ev.Error.Code, ev.Error.Message):
				default:
				}
				return
			}
			if ev.Data.URL != "" {
				announced = true
				select {
				case found <- ev.Data.URL:
				default:
				}
				// Keep draining: a full pipe buffer would block the server.
			}
		}
		// Stdout ended. If it ended BEFORE the listening event, say so now:
		// otherwise every test waits out the full boot timeout to learn that
		// a server which had already died was never going to answer.
		if announced {
			return
		}
		err := scanner.Err()
		if err == nil {
			err = fmt.Errorf("stdout closed without a listening event")
		}
		select {
		case failed <- fmt.Errorf("server stopped before it began serving: %w", err):
		default:
		}
	}()

	select {
	case url := <-found:
		return url, nil
	case err := <-failed:
		return "", err
	case <-time.After(bootTimeout):
		return "", fmt.Errorf("no listening event within %s", bootTimeout)
	}
}

func (s *server) record(line string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if len(s.logLine) < 200 {
		s.logLine = append(s.logLine, line)
	}
}

func (s *server) lines() []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	out := make([]string, len(s.logLine))
	copy(out, s.logLine)
	return out
}

func (s *server) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	done := make(chan struct{})
	go func() { _, _ = s.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// ctx bounds every request so a hung server fails the test rather than the run.
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}
