//go:build acceptance

// Package setupghcli_test exercises setup-github-cli against a real GitHub
// releases download. Tagged out of `task test` because it needs network. The CLI
// contract itself is covered hermetically by clitest/.
package setupghcli_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	p := compiler.NewParallel(1)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &binaryPath,
		Name:   "setup-github-cli",
		Target: "..",
		Source: ".",
	})

	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

func sandbox(t *testing.T) (*sdktest.Sandbox, func(*testing.T, map[string]string, ...string) (string, error)) {
	t.Helper()
	s := sdktest.NewSandbox(t)
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		env := s.Env()
		maps.Copy(env, extraEnv)
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(env))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}
